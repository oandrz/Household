// renderWithRouter, not a bare render: it is what supplies the
// QueryClientProvider useAgreements() needs (and a router for anything that
// ever grows a <Link>). Every fixture below is WRAPPED -- the wire is
// { "agreements": {…} } and { "proposal": {…}, "agreements": {…} } -- because
// the schemas parse the envelope and return the inner object.
import { useState } from "react";
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { ProposeAgreementModal, type AgreementProposeSeed } from "./ProposeAgreementModal";
import type { AgreementSection } from "./agreementSchemas";

const SECTIONS = [
  {
    id: "s-money",
    name: "Money",
    count: 1,
    visible: true,
    agreements: [{ id: "a-1", number: 1, body: "Each gets S$200/mo no-questions-asked" }],
  },
];

const OWNERS = [
  { membershipId: "m-a", name: "Andreas" },
  { membershipId: "m-c", name: "Christine" },
];

const doc = (version: number) => ({
  agreements: {
    locked: false,
    owners: OWNERS,
    version,
    updatedAt: null,
    sections: SECTIONS,
    proposals: [],
    history: [],
  },
});

function renderModal(
  seed: AgreementProposeSeed,
  routes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  stubFetchRoutes({
    "GET /api/v1/marriage/agreements": { status: 200, body: doc(2) },
    ...routes,
  });
  return renderWithRouter(
    <ProposeAgreementModal
      seed={seed}
      coOwnerNames={["Christine"]}
      sections={SECTIONS}
      onOpenNewSection={() => {}}
      onClose={() => {}}
    />,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

// A real parent re-render with a NEW `sections` array, not a fake rerender
// hack -- this is what AgreementsPage actually does the instant its own
// useAgreements() query refetches (a background invalidation from another
// owner's write landing, or the household's own periodic refetch) while
// this modal stays open and mounted. The button is the test's own trigger,
// standing in for "a refetch just landed."
function HarnessWithSwappableSections({
  seed,
  initialSections,
  movedSections,
}: {
  seed: AgreementProposeSeed;
  initialSections: AgreementSection[];
  movedSections: AgreementSection[];
}) {
  const [sections, setSections] = useState(initialSections);
  return (
    <>
      <button onClick={() => setSections(movedSections)}>simulate a landed refetch</button>
      <ProposeAgreementModal
        seed={seed}
        coOwnerNames={["Christine"]}
        sections={sections}
        onOpenNewSection={() => {}}
        onClose={() => {}}
      />
    </>
  );
}

describe("ProposeAgreementModal", () => {
  // Real radios, checked from the seed and never from a hardcoded "edit"
  // (spec, The modals). RetroModal.tsx's mood picker is the shape: visible
  // inputs sharing one `name`, NOT sr-only inputs behind a styled stand-in --
  // that shape shipped keyboard-invisible focus once already, and
  // fireEvent.click never presses a key, so no unit test can catch it.
  it("the chips are three real radios checked from the seed, and remove warns", async () => {
    renderModal({ mode: "remove", targetAgreementId: "a-1" });

    const group = await screen.findByRole("radiogroup", { name: "Change type" });
    expect(within(group).getAllByRole("radio")).toHaveLength(3);
    expect(screen.getByRole("radio", { name: "Remove" })).toBeChecked();
    // "Why (optional)" renders in all three modes (spec, The modals).
    expect(screen.getByLabelText("Why (optional)")).toBeInTheDocument();
    expect(screen.getByTestId("agreement-remove-warning")).toHaveTextContent(
      'This will be removed once Christine agrees. It stays in Version history, ' +
        "so you can always see it was there and restore it later.",
    );
  });

  // Two claims in one flow, because they are the same defect from both sides:
  // an edit pre-fills its wording, and switching mode CLEARS it. A hidden
  // stale value that still submits is worse than an empty one. The send is
  // asserted with toEqual against the WHOLE body -- toMatchObject would pass
  // a propose that dropped its note.
  it("edit pre-fills the wording, switching mode clears it, and the send carries all six fields", async () => {
    let sent: unknown;
    renderModal(
      { mode: "edit", targetAgreementId: "a-1" },
      {
        "POST /api/v1/marriage/agreements/proposals": {
          status: 201,
          body: {
            proposal: {
              id: "p-1",
              kind: "add",
              status: "pending",
              sectionId: "s-money",
              sectionName: "Money",
              targetAgreementId: "",
              body: "Screens off at meals",
              previousBody: "",
              note: "The table is for us",
              parkNote: "",
              proposedByMembershipId: "m-a",
              proposedByName: "Andreas",
              proposedAt: "2026-09-05T09:00:00Z",
              awaitingNames: ["Christine"],
              targetChanged: false,
              canAgree: false,
              canWithdraw: true,
            },
            agreements: doc(2).agreements,
          },
          capture: (body) => {
            sent = body;
          },
        },
      },
    );

    expect(await screen.findByLabelText("New wording")).toHaveValue(
      "Each gets S$200/mo no-questions-asked",
    );

    fireEvent.click(screen.getByRole("radio", { name: "Add new" }));
    expect(screen.getByLabelText("New agreement")).toHaveValue("");

    fireEvent.change(screen.getByLabelText("New agreement"), {
      target: { value: "Screens off at meals" },
    });
    fireEvent.change(screen.getByLabelText("Why (optional)"), {
      target: { value: "The table is for us" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send for agreement" }));

    await waitFor(() =>
      expect(sent).toEqual({
        kind: "add",
        sectionId: "s-money",
        targetAgreementId: "",
        body: "Screens off at meals",
        previousBody: "",
        note: "The table is for us",
      }),
    );
  });

  // The latch (decision 13; spec, "Propose answers 409 AGREEMENT_CHANGED of
  // its own"). The two GET fixtures MUST differ: identical bodies are
  // structurally shared by TanStack Query, `data` keeps its reference and
  // Step 5's mutation would never re-run.
  it("a 409 latches Send off, and it stays off across the refetch it triggers", async () => {
    const { queryClient } = renderModal(
      { mode: "edit", targetAgreementId: "a-1" },
      {
        "GET /api/v1/marriage/agreements": [
          { status: 200, body: doc(2) },
          { status: 200, body: doc(3) },
        ],
        "POST /api/v1/marriage/agreements/proposals": {
          status: 409,
          body: { error: { code: "AGREEMENT_CHANGED", message: "This agreement changed." } },
        },
      },
    );

    fireEvent.change(await screen.findByLabelText("New wording"), {
      target: { value: "Each gets S$250/mo no-questions-asked" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send for agreement" }));

    expect(await screen.findByTestId("agreement-propose-conflict")).toHaveTextContent(
      "This agreement changed while you were writing, so nothing was saved.",
    );

    // The refetch must have LANDED, not merely been requested: a route's
    // `capture` fires when the request is ISSUED, so counting requests would
    // run the next line one commit too early -- which is exactly the commit
    // in which Step 5's mutation clears the latch, leaving the check green
    // against broken code. Reading the cache also means this file never
    // imports Task 9's query-key name.
    await waitFor(() =>
      expect(
        queryClient
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ).toContainEqual(expect.objectContaining({ version: 3 })),
    );

    expect(screen.getByRole("button", { name: "Send for agreement" })).toBeDisabled();
    // Nothing typed is lost -- criterion 10's half that jsdom can express.
    expect(screen.getByLabelText("New wording")).toHaveValue(
      "Each gets S$250/mo no-questions-asked",
    );
  });

  // The Task 18 walk's own finding: a background refetch reaching this
  // modal while it stays open must never change what `previousBody` sends,
  // because previousBody's whole job is "the wording this session actually
  // saw" (this file's own comment on `bodyOf`). Before the fix, `handleSend`
  // called `bodyOf(targetId)` fresh at send time -- reading straight off the
  // `sections` PROP -- so once a target's own id was retired (exactly what
  // an edit or a remove does to it, decision 9) that call silently returned
  // "", which the server's own shape CHECK refuses outright rather than
  // answering the staleness conflict this exact situation should raise.
  it("previousBody is the wording seen when the target was chosen, not re-read from a sections prop that moved under an open modal", async () => {
    let sent: unknown;
    stubFetchRoutes({
      "GET /api/v1/marriage/agreements": { status: 200, body: doc(2) },
      "POST /api/v1/marriage/agreements/proposals": {
        status: 201,
        body: {
          proposal: {
            id: "p-1",
            kind: "edit",
            status: "pending",
            sectionId: "s-money",
            sectionName: "Money",
            targetAgreementId: "a-2",
            body: "My own new wording",
            previousBody: "Each gets S$200/mo no-questions-asked",
            note: "",
            parkNote: "",
            proposedByMembershipId: "m-a",
            proposedByName: "Andreas",
            proposedAt: "2026-09-05T09:00:00Z",
            awaitingNames: ["Christine"],
            targetChanged: false,
            canAgree: false,
            canWithdraw: true,
          },
          agreements: doc(2).agreements,
        },
        capture: (body) => {
          sent = body;
        },
      },
    });

    renderWithRouter(
      <HarnessWithSwappableSections
        seed={{ mode: "edit", targetAgreementId: "a-1" }}
        initialSections={SECTIONS}
        // Same position, same display number, a DIFFERENT id and body --
        // exactly what applyAgreementChange's remove()-then-add() leaves an
        // edit or a remove looking like from the outside.
        movedSections={[
          {
            id: "s-money",
            name: "Money",
            count: 1,
            visible: true,
            agreements: [{ id: "a-2", number: 1, body: "Someone else's landed wording" }],
          },
        ]}
      />,
    );

    expect(await screen.findByLabelText("New wording")).toHaveValue(
      "Each gets S$200/mo no-questions-asked",
    );

    // The refetch lands while this modal stays open and untouched.
    fireEvent.click(screen.getByRole("button", { name: "simulate a landed refetch" }));

    // The typed draft is unaffected by the prop swap -- still the original
    // pre-fill, not the newly-landed wording and not blanked either.
    expect(screen.getByLabelText("New wording")).toHaveValue(
      "Each gets S$200/mo no-questions-asked",
    );

    fireEvent.click(screen.getByRole("button", { name: "Send for agreement" }));

    await waitFor(() =>
      expect(sent).toMatchObject({
        // The id this session actually chose, not the section's new one --
        // targetId is untouched by the prop swap, same as body.
        targetAgreementId: "a-1",
        previousBody: "Each gets S$200/mo no-questions-asked",
      }),
    );
  });
});
