// The income panel's failure paths, which are the ones a happy-path walk in the
// browser never sees.
//
// The delete case is here because it shipped silent: the confirm button awaited
// the mutation with no catch, so a rejected delete left the row on screen, the
// panel stuck in its confirm state, nothing said, and an unhandled rejection in
// the console. HoldingLotsPanel.tsx -- which this file's header says it mirrors
// -- had caught it correctly all along.
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { HoldingIncomePanel } from "./HoldingIncomePanel";
import type { Holding } from "./holdingSchemas";

const holding: Holding = {
  id: "h1",
  accountId: "a1",
  accountName: "Moomoo SG",
  name: "Gold bar",
  instrument: "gold",
  unit: "gram",
  currency: "SGD",
  held: "49.5",
  heldNano: 49500000000,
  costMinor: 594000,
  marketValueMinor: 643500,
  hasMarketValue: true,
  primaryMarketValueMinor: 643500,
  primaryCurrency: "SGD",
  realisedMinor: 100000,
  valuedAt: "2026-07-10",
  archivedAt: null,
};

const oneRow = {
  income: [
    {
      id: "i1",
      kind: "income",
      amountMinor: 4500,
      currency: "SGD",
      primaryAmountMinor: null,
      primaryCurrency: null,
      receivedOn: "2026-07-02",
      note: "Q3 dividend",
    },
  ],
};

// The currency table carries `name` as well as `symbol`: leaving it out makes
// the schema throw, the symbol lookup return undefined, and every figure render
// as "SGD 45.00". That exact omission is how a milestone-1 defect reached the
// product, so the stub here carries the whole row.
const currencies = {
  currencies: [{ code: "SGD", name: "Singapore Dollar", symbol: "S$" }],
};

// stubFetchRoutes answers every request at once, so a test built on it never
// sees a request still on its way. This keeps it for the GETs and holds every
// DELETE open until the test calls `release`: that open window is where a
// person's second click lands. Every DELETE URL is recorded, so the test can
// count them.
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
  vi.restoreAllMocks();
});

describe("HoldingIncomePanel", () => {
  // A double click on "Really remove" used to send two DELETEs, because the
  // button stayed clickable while the first was still on its way.
  // TransactionModal, GoalContributionsPanel and BillRow already disable their
  // confirm buttons for the same reason.
  it("sends only one DELETE when the confirm button is clicked twice", async () => {
    const network = holdDeletesOpen({
      "GET /api/v1/currencies": { status: 200, body: currencies },
      "GET /api/v1/holdings/h1/income": { status: 200, body: oneRow },
    });
    renderWithRouter(<HoldingIncomePanel holding={holding} onClose={() => {}} />);

    await screen.findByText(/Q3 dividend/);
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    const confirmButton = screen.getByRole("button", { name: "Really remove" });
    fireEvent.click(confirmButton);
    fireEvent.click(confirmButton);

    await waitFor(() => expect(network.deletes).toHaveLength(1));
    // The count alone could pass too early: react-query sends the request a
    // few microtasks after the click, so a second DELETE might not have landed
    // yet. A disabled button is what actually stops the second click.
    expect(confirmButton).toBeDisabled();

    // Let the DELETE finish, and wait for the row to settle back to its plain
    // Remove button, so that any second request has had every chance to go.
    network.release();
    await waitFor(() => expect(screen.getByRole("button", { name: "Remove" })).toBeInTheDocument());
    expect(network.deletes).toEqual(["/api/v1/holdings/h1/income/i1"]);
  });

  it("says so when a delete fails, and keeps the row", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": { status: 200, body: currencies },
      "GET /api/v1/holdings/h1/income": { status: 200, body: oneRow },
      "DELETE /api/v1/holdings/h1/income/i1": {
        status: 422,
        body: { error: { code: "INCOME_LOCKED", message: "That entry cannot be removed." } },
      },
    });
    renderWithRouter(<HoldingIncomePanel holding={holding} onClose={() => {}} />);

    await screen.findByText(/Q3 dividend/);
    fireEvent.click(screen.getByRole("button", { name: "Remove" }));
    fireEvent.click(screen.getByRole("button", { name: "Really remove" }));

    // The server's own sentence, not a generic one: the person can only act on
    // the reason, and apiErrorMessage exists to carry it.
    expect(await screen.findByRole("alert")).toHaveTextContent("That entry cannot be removed.");
    // Still there. A row that vanishes from a failed delete is the worse half
    // of this bug -- the screen would disagree with the database until a
    // refetch.
    expect(screen.getByText(/Q3 dividend/)).toBeInTheDocument();
    // And the confirm state is cleared, so the button is usable again rather
    // than frozen mid-confirmation.
    await waitFor(() => expect(screen.getByRole("button", { name: "Remove" })).toBeInTheDocument());
  });

  it("shows the amount with the household's symbol", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": { status: 200, body: currencies },
      "GET /api/v1/holdings/h1/income": { status: 200, body: oneRow },
    });
    renderWithRouter(<HoldingIncomePanel holding={holding} onClose={() => {}} />);

    expect(await screen.findByText(/S\$45\.00/)).toBeInTheDocument();
  });
});
