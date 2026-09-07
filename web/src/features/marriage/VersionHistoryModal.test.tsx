import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { VersionHistoryModal } from "./VersionHistoryModal";

const entry = (version: number, over: Record<string, unknown> = {}) => ({
  version,
  proposalId: `p-${version}`,
  kind: "add",
  sectionId: "s-money",
  sectionName: "Money",
  body: `Agreement ${version}`,
  previousBody: "",
  note: "",
  proposedByName: "Andreas",
  signedByNames: ["Andreas", "Christine"],
  acceptedAt: "2026-06-28T09:00:00Z",
  ...over,
});

function renderHistory(history: unknown[], version: number) {
  const onRestore = vi.fn();
  stubFetchRoutes({
    "GET /api/v1/marriage/agreements": {
      status: 200,
      body: {
        agreements: {
          locked: false,
          owners: [
            { membershipId: "m-a", name: "Andreas" },
            { membershipId: "m-c", name: "Christine" },
          ],
          version,
          updatedAt: "2026-06-28T09:00:00Z",
          sections: [],
          proposals: [],
          history,
        },
      },
    },
  });
  renderWithRouter(<VersionHistoryModal onRestore={onRestore} onClose={() => {}} />);
  return onRestore;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("VersionHistoryModal", () => {
  // Deliberately mis-ordered: nothing in the schema enforces the server's
  // newest-first ordering, so a document whose newest change is not history[0]
  // must still crown the right row (spec, "…its disclosure").
  it("marks current the row whose version equals the document's, not the first", async () => {
    renderHistory([entry(3), entry(4), entry(2)], 4);

    expect(await screen.findByText("v4 · current")).toBeInTheDocument();
    expect(screen.getByText("v3")).toBeInTheDocument();
  });

  // Four render, two collapse, and BOTH bounds come from the ones that
  // collapsed. There is no v1 row: a document is v1 before anything is agreed,
  // so the first accepted proposal produces v2 (decision 10) and a hardcoded 1
  // is wrong by construction.
  it("collapses all but the four newest, bounded by what actually collapsed", async () => {
    renderHistory([entry(7), entry(6), entry(5), entry(4), entry(3), entry(2)], 7);

    expect(await screen.findByRole("button", { name: "Show v2–v3 ↓" })).toBeInTheDocument();
    expect(screen.queryByText("v3")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show v2–v3 ↓" }));
    expect(screen.getByText("v3")).toBeInTheDocument();
  });

  // The sentence is derived from `kind` with no guessing default, and Restore
  // is offered on removals only (decision 18) -- an edit back is one click away
  // through the ordinary flow.
  it("derives the sentence from the kind, and offers Restore on removals only", async () => {
    const onRestore = renderHistory(
      [
        entry(4, { kind: "remove", body: "", previousBody: "No phones at dinner" }),
        entry(3, { kind: "edit", previousBody: "Old wording" }),
        entry(2),
      ],
      4,
    );

    const row = await screen.findByTestId("agreement-history-p-4");
    expect(row).toHaveTextContent('Removed from Money: "No phones at dinner"');
    // joinNames joins with " and ", not the design's "&" (plan header,
    // Conventions) -- one join, one assertion, everywhere.
    expect(row).toHaveTextContent("Agreed by Andreas and Christine");
    expect(screen.getByTestId("agreement-history-p-3")).toHaveTextContent(
      'Changed in Money: "Agreement 3"',
    );

    const restoreButtons = screen.getAllByRole("button", { name: "Restore" });
    expect(restoreButtons).toHaveLength(1);
    fireEvent.click(restoreButtons[0]);
    expect(onRestore).toHaveBeenCalledWith({
      mode: "add",
      body: "No phones at dinner",
      sectionId: "s-money",
    });
  });
});
