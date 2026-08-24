// Pins the change badge's own *arrangement*, not just its words -- the
// wording ("▲ 2.1%" plus, on Overview, "this month") was already identical
// between the inline and block layouts, so a text-only assertion could not
// have caught the round this test exists because of: the badge wrapped
// mid-phrase at 360px/320px on Overview when it stayed inline beside the
// 30px figure (docs/LEARNING.md pattern 3). NetWorthCard itself decides the
// arrangement from whether `changeNote` is given -- Finances never passes
// one, Overview always does -- so these tests drive that same prop split
// rather than mounting either page whole.
//
// renderWithRouter for a fresh QueryClient (this card calls useCurrencies()
// internally) and stubFetchRoutes for the one request that triggers, per
// BudgetRolloverCard.test.tsx's own convention.
import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { NetWorthCard } from "./NetWorthCard";
import type { Summary } from "./schemas";

afterEach(() => vi.unstubAllGlobals());

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

function summaryFixture(overrides: Partial<Summary> = {}): Summary {
  return {
    currency: "SGD",
    computable: true,
    netWorthMinor: 4700000,
    assetsMinor: 4700000,
    liabilitiesMinor: 0,
    breakdown: [],
    excludedNoRate: [],
    excludedByChoice: 0,
    trend: { points: [], changeBasisPoints: -600 },
    ...overrides,
  } as Summary;
}

describe("NetWorthCard's change badge", () => {
  it("with a changeNote (Overview), renders the change as its own block underneath the figure, not inline beside it", async () => {
    stubFetchRoutes({ "GET /api/v1/currencies": CURRENCIES });
    renderWithRouter(<NetWorthCard summary={summaryFixture()} changeNote="this month" />);

    // findByText, not getByText: the figure's symbol comes from a second
    // query (useCurrencies) that has not necessarily resolved by the first
    // render, so the figure briefly reads "SGD 47,000.00" (formatMoney's own
    // no-symbol fallback) before settling on "S$47,000.00".
    // getByText matches only against a node's own direct text-node children
    // (Testing Library's default getNodeText), so this returns the figure's
    // <p> itself regardless of which layout is active -- a nested <span>'s
    // own text never leaks into its parent's match here.
    const figure = await screen.findByText("S$47,000.00");
    const change = screen.getByTestId("net-worth-change");

    // The block layout: a <p>, and a sibling of the figure's own <p> --
    // sharing the same parent (the card's <section>) -- rather than a child
    // nested inside it.
    expect(change.tagName).toBe("P");
    expect(change.parentElement).toBe(figure.parentElement);
    expect(figure.contains(change)).toBe(false);

    expect(change).toHaveTextContent("▼ 6.0% this month");
  });

  it("with no changeNote (Finances), keeps the change inline inside the figure's own line", async () => {
    stubFetchRoutes({ "GET /api/v1/currencies": CURRENCIES });
    renderWithRouter(<NetWorthCard summary={summaryFixture()} />);

    const figure = await screen.findByText("S$47,000.00");
    const change = screen.getByTestId("net-worth-change");

    // The inline layout: a <span>, nested inside the figure's own <p> --
    // the figure element is the span's direct parent.
    expect(change.tagName).toBe("SPAN");
    expect(change.parentElement).toBe(figure);
    expect(figure.contains(change)).toBe(true);

    expect(change).toHaveTextContent("▼ 6.0%");
    expect(change).not.toHaveTextContent("this month");
  });

  it("renders no change badge at all when changeBasisPoints is absent, with a changeNote", async () => {
    stubFetchRoutes({ "GET /api/v1/currencies": CURRENCIES });
    const summary = summaryFixture({ trend: { points: [], changeBasisPoints: undefined } });
    renderWithRouter(<NetWorthCard summary={summary} changeNote="this month" />);

    await screen.findByText("S$47,000.00");
    expect(screen.queryByTestId("net-worth-change")).not.toBeInTheDocument();
  });

  it("renders no change badge at all when changeBasisPoints is absent, with no changeNote", async () => {
    stubFetchRoutes({ "GET /api/v1/currencies": CURRENCIES });
    const summary = summaryFixture({ trend: { points: [], changeBasisPoints: undefined } });
    renderWithRouter(<NetWorthCard summary={summary} />);

    await screen.findByText("S$47,000.00");
    expect(screen.queryByTestId("net-worth-change")).not.toBeInTheDocument();
  });

  it("colours a rising change with the accent token, block layout included", async () => {
    stubFetchRoutes({ "GET /api/v1/currencies": CURRENCIES });
    const summary = summaryFixture({ trend: { points: [], changeBasisPoints: 210 } });
    renderWithRouter(<NetWorthCard summary={summary} changeNote="this month" />);

    const change = await screen.findByTestId("net-worth-change");
    expect(change).toHaveTextContent("▲ 2.1% this month");
    expect(change.className).toContain("text-accent");
    expect(change.className).not.toContain("text-danger");
  });
});
