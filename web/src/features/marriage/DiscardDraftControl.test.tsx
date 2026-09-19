// DiscardDraftControl on its own, for the path RetroModal.test.tsx never
// walks: a discard the server refuses.
//
// Rendered without RetroModal around it, so the DELETE is the only request and
// nothing else needs stubbing. `discardDraft` calls apiFetch against the
// stubbed route rather than being a rejecting vi.fn, so the error that reaches
// the control is a real ApiError built from a real response -- the same thing
// useRetro's own discardDraft hands it.
import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { apiFetch } from "../../api/client";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { DiscardDraftControl } from "./DiscardDraftControl";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("DiscardDraftControl", () => {
  // useConfirmAction collapses the confirm pair once the request settles,
  // success or failure. The error used to be drawn INSIDE that pair, so a
  // refused discard showed nothing: the pair closed, the draft stayed, and the
  // reason only appeared if the person clicked "Discard draft" a second time.
  it("shows why a discard failed straight away, with the Discard draft button back", async () => {
    stubFetchRoutes({
      // The draft is already gone -- discarded in the other partner's tab.
      "DELETE /api/v1/retros/2026-07": {
        status: 404,
        body: { error: { code: "NOT_FOUND", message: "That retro no longer exists." } },
      },
    });
    const onClose = vi.fn();
    renderWithRouter(
      <DiscardDraftControl
        disabled={false}
        discardDraft={async () => {
          await apiFetch<unknown>("/api/v1/retros/2026-07", { method: "DELETE" });
        }}
        onClose={onClose}
      />,
    );

    fireEvent.click(await screen.findByRole("button", { name: "Discard draft" }));
    fireEvent.click(screen.getByRole("button", { name: "Yes, discard it" }));

    // Nothing else is clicked between the confirm and these assertions.
    expect(await screen.findByRole("alert")).toHaveTextContent("That retro no longer exists.");
    expect(screen.getByRole("button", { name: "Discard draft" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Yes, discard it" })).not.toBeInTheDocument();
    // A failed discard must not close the modal as if it had worked.
    expect(onClose).not.toHaveBeenCalled();
  });
});
