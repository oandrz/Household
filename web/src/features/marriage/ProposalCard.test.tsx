// Props in, nothing else: no router, no network -- PillarCard.test.tsx's shape.
// fireEvent, never userEvent: @testing-library/user-event is not a dependency
// of this project and Global Constraints forbid adding one.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProposalCard } from "./ProposalCard";
import type { AgreementProposal } from "./agreementSchemas";

// The window.confirm spy in the withdraw case would otherwise leak into every
// test that runs after it.
afterEach(() => {
  vi.restoreAllMocks();
});

// Local, because the shared fixtures live in AgreementsPage.test.tsx and
// importing from another test file would re-run that file's suite. Same
// defaults as Task 9's, so a reader comparing the two is comparing like
// with like.
function proposalFixture(o: Partial<AgreementProposal> = {}): AgreementProposal {
  return {
    id: "p-1",
    kind: "add",
    status: "pending",
    sectionId: "s-1",
    sectionName: "Money",
    targetAgreementId: "",
    body: "Any purchase over S$500 gets discussed first.",
    previousBody: "",
    note: "",
    parkNote: "",
    proposedByMembershipId: "m-1",
    proposedByName: "Andreas",
    proposedAt: "2026-07-14T09:00:00+08:00",
    awaitingNames: ["Christine"],
    targetChanged: false,
    canAgree: true,
    canWithdraw: false,
    ...o,
  };
}

function renderCard(o: Partial<AgreementProposal> = {}, targetNumber: number | null = null) {
  const onAgree = vi.fn<(id: string) => Promise<string | null>>().mockResolvedValue(null);
  const onPark = vi.fn<(id: string, note: string) => Promise<string | null>>().mockResolvedValue(null);
  const onWithdraw = vi.fn<(id: string) => Promise<string | null>>().mockResolvedValue(null);
  const view = render(
    <ProposalCard
      proposal={proposalFixture(o)}
      targetNumber={targetNumber}
      onAgree={onAgree}
      onPark={onPark}
      onWithdraw={onWithdraw}
    />,
  );
  return { ...view, onAgree, onPark, onWithdraw };
}

// [case, overrides, buttons that must be there, buttons that must not]
const matrix: [string, Partial<AgreementProposal>, string[], string[]][] = [
  ["the proposer, waiting on the other owner", { canAgree: false, canWithdraw: true }, ["Withdraw"], ["Agree", "Discuss"]],
  ["the other owner on a pending change", { canAgree: true, canWithdraw: false }, ["Agree", "Discuss"], ["Withdraw"]],
  ["the other owner on a parked change", { status: "parked", canAgree: true, canWithdraw: false }, ["Agree"], ["Discuss", "Withdraw"]],
  ["everyone still here has already agreed", { canAgree: true, canWithdraw: true, awaitingNames: [] }, ["Agree", "Withdraw"], ["Discuss"]],
  // Decision 16's trap, and the reason canAgree alone decides Agree: canAgree
  // is `!locked && ...`, so a locked household reaches this row with an empty
  // awaiting list and must be offered nothing. A test on
  // `awaitingNames.length === 0` alone would put an Agree button on a page
  // whose every write refuses with 409.
  ["a locked household whose awaiting list emptied", { canAgree: false, canWithdraw: false, awaitingNames: [] }, [], ["Agree", "Discuss", "Withdraw"]],
];

describe("ProposalCard", () => {
  it.each(matrix)("offers the right actions to %s", (_case, overrides, shown, hidden) => {
    renderCard(overrides);

    shown.forEach((label) => expect(screen.getByRole("button", { name: label })).toBeInTheDocument());
    hidden.forEach((label) => expect(screen.queryByRole("button", { name: label })).not.toBeInTheDocument());
  });

  // doc.proposals excludes accepted and withdrawn in SQL, so a fourth status
  // here is a row nothing wrote. The card refuses rather than guessing a title
  // for it -- a write response may legitimately carry one, and no screen
  // renders that field.
  it("renders nothing at all for a status the document never carries", () => {
    expect(renderCard({ status: "accepted" }).container).toBeEmptyDOMElement();
  });

  it("titles a pending change with everyone it waits for, and an edit shows both wordings", () => {
    renderCard(
      {
        kind: "edit",
        targetAgreementId: "a-2",
        previousBody: "Each gets S$200/mo. no questions asked.",
        body: "Each gets S$250/mo. no questions asked.",
        awaitingNames: ["Christine", "Ibu"],
      },
      2,
    );

    // joinNames joins with "and", not the design's "&" -- one function, three
    // call sites, so three owners cannot read correctly here and wrongly in
    // version history. The dash is an em dash (U+2014).
    expect(screen.getByText("Pending change — needs Christine and Ibu")).toBeInTheDocument();
    expect(screen.getByText(/Andreas proposed changing Money 02:/)).toBeInTheDocument();
    expect(screen.getByTestId("proposal-previous-body")).toHaveClass("line-through");
    expect(screen.getByTestId("proposal-body")).toHaveTextContent("S$250");
  });

  // Decision 16 again, from the other side: the sentence that explains why
  // Agree is still offered when nobody is being waited on.
  it("explains why Agree is still offered once the awaiting list has emptied", () => {
    renderCard({ canAgree: true, awaitingNames: [] });

    expect(screen.getByText("Pending change")).toBeInTheDocument();
    expect(screen.getByTestId("proposal-everyone-agreed")).toHaveTextContent(
      "Everyone still here has agreed — Agree once more to make it final.",
    );
  });

  it("shows a park note under the To discuss label, and nothing when the note is empty", () => {
    const { rerender, onAgree, onPark, onWithdraw } = renderCard({ status: "parked", parkNote: "The ceiling feels low" });

    expect(screen.getByTestId("proposal-park-note")).toHaveTextContent("To discuss The ceiling feels low");
    // An empty park note is ordinary, not a defect: Discuss is a bare button
    // and decision 7 stores whatever was typed, including nothing.
    rerender(
      <ProposalCard
        proposal={proposalFixture({ status: "parked", parkNote: "" })}
        targetNumber={null}
        onAgree={onAgree}
        onPark={onPark}
        onWithdraw={onWithdraw}
      />,
    );
    expect(screen.queryByTestId("proposal-park-note")).not.toBeInTheDocument();
  });

  // Decision 14: the sentence depends on who is reading, never on who
  // proposed it -- canWithdraw already carries decision 15's fallback, so a
  // household whose proposer has left is never told to ask a ghost.
  it.each([
    [{ canWithdraw: true }, "Withdraw it and propose the change again."],
    [{ canWithdraw: false, proposedByName: "Andreas" }, "Ask Andreas to withdraw it and propose it again against the current wording."],
    [{ canWithdraw: false, proposedByName: "" }, "This needs withdrawing and proposing again against the current wording."],
  ] as [Partial<AgreementProposal>, string][])("addresses a stale proposal to whoever is reading (%#)", (overrides, sentence) => {
    renderCard({ targetChanged: true, canAgree: true, ...overrides });

    expect(screen.getByTestId("proposal-stale-note")).toHaveTextContent(sentence);
    expect(screen.getByRole("button", { name: "Agree" })).toBeDisabled();
  });

  it("expands Discuss inside the card and parks with the note typed there", async () => {
    const { onPark } = renderCard({ canAgree: true, canWithdraw: false });

    fireEvent.click(screen.getByRole("button", { name: "Discuss" }));
    fireEvent.change(screen.getByLabelText("What you want to talk through (optional)"), {
      target: { value: "The ceiling feels low" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Park for next retro" }));

    await waitFor(() => expect(onPark).toHaveBeenCalledWith("p-1", "The ceiling feels low"));
    // The panel closes on success, so the note cannot be sent twice.
    await waitFor(() => expect(screen.queryByTestId("proposal-discuss")).not.toBeInTheDocument());
  });

  it("confirms a withdrawal in the card, never through window.confirm, and shows a failure in place", async () => {
    const confirmSpy = vi.spyOn(window, "confirm");
    const { onWithdraw } = renderCard({ canWithdraw: true });
    onWithdraw.mockResolvedValue("Someone changed this while you were reading.");

    fireEvent.click(screen.getByRole("button", { name: "Withdraw" }));

    expect(onWithdraw).not.toHaveBeenCalled();
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(screen.getByTestId("proposal-withdraw-confirm")).toHaveTextContent(
      "Withdraw this proposal? It stops waiting for anyone, and nothing in the document changes.",
    );

    fireEvent.click(screen.getByRole("button", { name: "Withdraw it" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Someone changed this while you were reading.");
    expect(onWithdraw).toHaveBeenCalledWith("p-1");
  });
});
