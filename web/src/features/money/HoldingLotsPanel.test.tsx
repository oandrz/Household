// today() (HoldingLotsPanel.tsx) must read the LOCAL calendar date rather than
// converting through UTC. This repo has already shipped three bugs of exactly
// that class -- f61407d ("stop refusing today's date east of UTC"), f17be2d
// ("fix dateOnly, the third instance of one mistake") and the plan correction
// behind both -- and AccountModal.test.tsx carries the same test for the same
// reason. This was the fourth site with the hazard and the second with no
// test; it shipped the bug, and a code review found it.
//
// `toFake: ["Date"]` freezes only what `new Date()` returns, leaving setTimeout
// alone, so the query client's own polling still resolves.
import { screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { HoldingLotsPanel } from "./HoldingLotsPanel";
import type { Holding } from "./holdingSchemas";

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

function holdingFixture(): Holding {
  return {
    id: "holding-1",
    accountId: "account-1",
    accountName: "Brokerage",
    name: "Gold bar",
    instrument: "gold",
    unit: "gram",
    currency: "SGD",
    archivedAt: null,
    heldNano: 0,
    held: "0",
    costMinor: 0,
    realisedMinor: 0,
    marketValueMinor: 0,
    hasMarketValue: false,
    valuedAt: null,
    primaryMarketValueMinor: null,
    primaryCurrency: null,
  };
}

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("HoldingLotsPanel", () => {
  it("defaults both dates to the local calendar day, not the UTC one", async () => {
    // Singapore is UTC+8, so at 16:00 UTC on 1 Jan it is already 2 Jan there.
    // A household recording a purchase just after midnight would otherwise
    // have it stamped the previous day -- and since ListLatestValuations
    // orders by as_of, a price stamped a day early can be silently outranked.
    process.env.TZ = "Asia/Singapore";
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-01-01T16:00:00Z"));

    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/holdings/holding-1/events": { status: 200, body: { events: [] } },
      "GET /api/v1/holdings/holding-1/valuations": { status: 200, body: { valuations: [] } },
    });

    renderWithRouter(<HoldingLotsPanel holding={holdingFixture()} onClose={() => {}} />);

    // The purchase date and the price date are separate inputs and both
    // default from the same helper, so both are asserted.
    expect(await screen.findByLabelText("On")).toHaveValue("2026-01-02");
    expect(screen.getByLabelText("As of")).toHaveValue("2026-01-02");
  });
});
