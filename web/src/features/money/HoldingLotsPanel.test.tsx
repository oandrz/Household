// The panel's two date fields default to the household's today, read in the
// household's own time zone and not the browser's (lib/householdDate.ts). The
// last test pins that, with the browser deliberately in another zone.
//
// `toFake: ["Date"]` freezes only what `new Date()` returns, leaving setTimeout
// alone, so the query client's own polling still resolves.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { meRoute } from "../../test/meFixture";
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

const ORIGINAL_TZ = process.env.TZ;

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  if (ORIGINAL_TZ === undefined) delete process.env.TZ;
  else process.env.TZ = ORIGINAL_TZ;
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

  // QA ISSUE-002, the browser's half. The form's default date and the date
  // the server accepts have to be the same day. Both are the household's
  // today, so the browser's own zone must not come into it.
  it("defaults both dates to the household's today, whatever zone the browser is in", async () => {
    // 23:00 UTC on 30 September: 16:00 that day in Los Angeles, where the
    // browser is, and 07:00 on 1 October in Singapore, where the household is.
    process.env.TZ = "America/Los_Angeles";
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-09-30T23:00:00Z"));

    stubFetchRoutes({
      ...meRoute("Asia/Singapore"),
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/holdings/holding-1/events": { status: 200, body: { events: [] } },
      "GET /api/v1/holdings/holding-1/valuations": { status: 200, body: { valuations: [] } },
    });

    renderWithRouter(<HoldingLotsPanel holding={holdingFixture()} onClose={() => {}} />);

    // The purchase date and the price date are separate inputs, so both are
    // asserted.
    await waitFor(() => expect(screen.getByLabelText("On")).toHaveValue("2026-10-01"));
    expect(screen.getByLabelText("As of")).toHaveValue("2026-10-01");
  });
});
