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
import { fireEvent, screen, waitFor } from "@testing-library/react";
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

const ONE_PURCHASE = {
  events: [
    {
      id: "event-1",
      kind: "acquisition",
      quantityNano: 10000000000,
      quantity: "10",
      amountMinor: 120000,
      currency: "SGD",
      primaryAmountMinor: null,
      primaryCurrency: null,
      occurredOn: "2026-07-01",
      note: "",
    },
  ],
};

// stubFetchRoutes answers every request at once, so a test built on it never
// sees a request still on its way. This keeps it for the GETs and holds every
// DELETE open until the test calls `release`: that open window is where a
// person's second click lands. Every DELETE URL is recorded, so the test can
// count them. HoldingIncomePanel.test.tsx carries the same helper; each file
// keeps its own copy rather than growing the shared fetch stub for two tests.
function holdDeletesOpen(routes: Parameters<typeof stubFetchRoutes>[0]) {
  const answerFromRoutes = stubFetchRoutes(routes);
  const deletes: string[] = [];
  let release: () => void = () => {};
  const heldResponse = new Promise<Response>((resolve) => {
    release = () => resolve(new Response(null, { status: 204 }));
  });
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) => {
    if ((init?.method ?? "GET").toUpperCase() === "DELETE") {
      deletes.push(String(input));
      return heldResponse;
    }
    return answerFromRoutes(input, init);
  });
  return { deletes, release: () => release() };
}

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

describe("HoldingLotsPanel", () => {
  // A double click on the confirming Remove used to send two DELETEs, because
  // the button stayed clickable while the first was still on its way.
  // TransactionModal, GoalContributionsPanel and BillRow already disable their
  // confirm buttons for the same reason.
  it("sends only one DELETE when the confirm button is clicked twice", async () => {
    const network = holdDeletesOpen({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/holdings/holding-1/events": { status: 200, body: ONE_PURCHASE },
      "GET /api/v1/holdings/holding-1/valuations": { status: 200, body: { valuations: [] } },
    });
    renderWithRouter(<HoldingLotsPanel holding={holdingFixture()} onClose={() => {}} />);

    await screen.findByText("2026-07-01");
    // Before asking there is one Remove (the trigger); once asked, the only
    // Remove on screen is the confirming one, beside Keep.
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    const confirmButton = screen.getByRole("button", { name: "Remove" });
    expect(screen.getByRole("button", { name: "Keep" })).toBeInTheDocument();
    fireEvent.click(confirmButton);
    fireEvent.click(confirmButton);

    await waitFor(() => expect(network.deletes).toHaveLength(1));
    // The count alone could pass too early: react-query sends the request a
    // few microtasks after the click, so a second DELETE might not have landed
    // yet. A disabled button is what actually stops the second click.
    expect(confirmButton).toBeDisabled();

    // Let the DELETE finish, and wait for the confirm pair to close, so that
    // any second request has had every chance to go.
    network.release();
    await waitFor(() => expect(screen.queryByRole("button", { name: "Keep" })).not.toBeInTheDocument());
    expect(network.deletes).toEqual(["/api/v1/holdings/holding-1/events/event-1"]);
  });

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
