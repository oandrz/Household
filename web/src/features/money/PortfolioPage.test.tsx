// Follows GoalsPage.test.tsx's shape: renderWithRouter plus stubFetchRoutes
// for every request, and literal strings asserted rather than an imported copy
// module (which would make the assertion tautological against a typo in that
// same module).
//
// Three of these tests exist for one reason: the page must render what the
// SERVER said, not what it could work out itself. hasMarketValue, valuedAt and
// notInNetWorth are all wire flags, and each has a branch that is easy to
// reimplement locally and get subtly wrong.
import { screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { PortfolioPage } from "./PortfolioPage";
import type { Holding } from "./holdingSchemas";

function holdingFixture(overrides: Partial<Holding> = {}): Holding {
  return {
    id: "holding-1",
    accountId: "account-1",
    accountName: "Brokerage",
    name: "Gold bar",
    instrument: "gold",
    unit: "gram",
    currency: "SGD",
    archivedAt: null,
    heldNano: 200_000_000_000,
    held: "200",
    costMinor: 2_199_000,
    realisedMinor: 0,
    marketValueMinor: 2_600_000,
    hasMarketValue: true,
    valuedAt: "2026-07-10",
    primaryMarketValueMinor: null,
    primaryCurrency: null,
    ...overrides,
  };
}

function portfolio(holdings: Holding[], notInNetWorth = true) {
  return { status: 200, body: { holdings, notInNetWorth } };
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("PortfolioPage", () => {
  it("renders the held quantity as the string the server sent, not a divided integer", async () => {
    // 100.5 + 99.5 grams. If this page ever computes held from heldNano it
    // does so in float64, which is the defect docs/LEARNING.md records the
    // budget split shipping once (333333 * 0.3 === 99999.90000000001).
    stubFetchRoutes({
      "GET /api/v1/holdings": portfolio([holdingFixture({ held: "300.5", heldNano: 300_500_000_000 })]),
    });
    renderWithRouter(<PortfolioPage />);

    expect(await screen.findByText("300.5 gram")).toBeInTheDocument();
  });

  it("says no price is recorded rather than showing a zero", async () => {
    stubFetchRoutes({
      "GET /api/v1/holdings": portfolio([
        holdingFixture({ hasMarketValue: false, marketValueMinor: 0, valuedAt: null }),
      ]),
    });
    renderWithRouter(<PortfolioPage />);

    expect(await screen.findByTestId("no-price")).toHaveTextContent("No price recorded");
    // A holding nobody has priced is unknowable, not worthless. S$0.00 would
    // be the wrong claim, and it is the one a naive render would make.
    expect(screen.queryByText("S$0.00")).toBeNull();
  });

  it("shows how stale a price is beside the figure", async () => {
    stubFetchRoutes({ "GET /api/v1/holdings": portfolio([holdingFixture()]) });
    renderWithRouter(<PortfolioPage />);

    expect(await screen.findByText(/as of 2026-07-10/)).toBeInTheDocument();
  });

  it("shows the household's own currency beside the instrument's when they differ", async () => {
    stubFetchRoutes({
      "GET /api/v1/holdings": portfolio([
        holdingFixture({
          name: "VOO",
          currency: "USD",
          marketValueMinor: 52000,
          primaryMarketValueMinor: 68000,
          primaryCurrency: "SGD",
        }),
      ]),
    });
    renderWithRouter(<PortfolioPage />);

    // Both figures: the native one says whether the pick was good, the primary
    // one says what it did to this household's wealth.
    expect(await screen.findByText(/520\.00/)).toBeInTheDocument();
    expect(screen.getByText(/680\.00/)).toBeInTheDocument();
  });

  it("shows one figure for a holding already in the household's currency", async () => {
    stubFetchRoutes({ "GET /api/v1/holdings": portfolio([holdingFixture()]) });
    renderWithRouter(<PortfolioPage />);

    await screen.findByText(/26,000\.00/);
    // primaryMarketValueMinor is null, so there is no second figure to show --
    // two identical numbers would read as a mistake.
    expect(screen.queryByText("≈")).toBeNull();
  });

  it("reads the not-in-net-worth banner from the server rather than hardcoding it", async () => {
    stubFetchRoutes({ "GET /api/v1/holdings": portfolio([holdingFixture()], true) });
    const { unmount } = renderWithRouter(<PortfolioPage />);
    expect(await screen.findByTestId("not-in-net-worth")).toBeInTheDocument();
    unmount();

    // The flag is the server's to change. When milestone 3 folds holdings into
    // net worth it flips there, and this banner has to disappear without a
    // frontend edit -- which is only true if the page reads it.
    stubFetchRoutes({ "GET /api/v1/holdings": portfolio([holdingFixture()], false) });
    renderWithRouter(<PortfolioPage />);
    await screen.findByText("Gold bar");
    expect(screen.queryByTestId("not-in-net-worth")).toBeNull();
  });

  it("offers the first-run empty state when nothing is held", async () => {
    stubFetchRoutes({ "GET /api/v1/holdings": portfolio([]) });
    renderWithRouter(<PortfolioPage />);

    expect(await screen.findByText("Nothing here yet")).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Add your first holding" })).toBeInTheDocument();
    });
  });

  it("labels an archived holding rather than hiding what it held", async () => {
    stubFetchRoutes({
      "GET /api/v1/holdings": portfolio([holdingFixture({ archivedAt: "2026-07-20T00:00:00Z" })]),
    });
    renderWithRouter(<PortfolioPage />);

    expect(await screen.findByText("(archived)")).toBeInTheDocument();
    // Archiving retires a position from view; it is not a claim that the
    // position was always empty.
    expect(screen.getByText("200 gram")).toBeInTheDocument();
  });
});
