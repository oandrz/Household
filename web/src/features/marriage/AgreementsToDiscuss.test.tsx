// The read-only "To discuss" block, and the claim the spec makes about it that
// no single-component test can reach: this block and AgreementsPage share ONE
// query key, so one Agree here refreshes both screens off one refetch. The
// second describe below is the only place in the suite where both consumers
// are mounted at once, which is why the spec's "assert agree's two call sites
// separately" and "both mounted components re-rendering off that one
// invalidation" land here rather than in Task 9's hook test.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { AgreementsToDiscuss } from "./AgreementsToDiscuss";
import { AgreementsPage } from "./AgreementsPage";
import type { AgreementProposal, AgreementsDocument } from "./agreementSchemas";
// Task 10's shared module, not a fourth copy of these four helpers. A local
// meFixture here drifted from Task 10's on two fields the first time this was
// written, which is the drift the shared module exists to prevent -- and
// documentFixture/proposalFixture here return the INNER document, so the
// wrapping below is still this file's job.
import { DOC_URL, ME_URL, documentFixture, meFixture, proposalFixture } from "./agreementFixtures";
const AGREE_URL = `POST ${DOC_URL}/proposals/prop-1/agree`;
const OWNERS = [
  { membershipId: "mem-1", name: "Andreas" },
  { membershipId: "mem-2", name: "Christine" },
];

// The one fixture this file adds, because no other task needs a parked
// proposal: everything else comes from ./agreementFixtures. id and body are
// set explicitly (not left at proposalFixture's own defaults of "p-1" and
// "One shared account for bills") because every row id, AGREE_URL and body
// assertion in this file is written against "prop-1" / "No solo spend over
// $200" -- proposalFixture's defaults exist for Task 10/12's own tests, not
// this one.
function parkedFixture(o: Partial<AgreementProposal> = {}): AgreementProposal {
  return proposalFixture({
    id: "prop-1",
    body: "No solo spend over $200",
    status: "parked",
    parkNote: "I want to talk about the number",
    ...o,
  });
}

function renderBlock(
  doc: AgreementsDocument,
  extra: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  const fetchMock = stubFetchRoutes({
    [`GET ${DOC_URL}`]: { status: 200, body: { agreements: doc } },
    ...extra,
  });
  return { fetchMock, ...renderWithRouter(<AgreementsToDiscuss />) };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("AgreementsToDiscuss", () => {
  it("lists the parked proposal and not the pending one, offers Agree and neither Discuss nor Withdraw, and Agree posts", async () => {
    let posted = false;
    renderBlock(
      documentFixture({
        proposals: [parkedFixture(), proposalFixture({ id: "prop-2", status: "pending" })],
      }),
      {
        [AGREE_URL]: {
          status: 200,
          body: {
            proposal: proposalFixture({ status: "accepted", awaitingNames: [], canAgree: false }),
            agreements: documentFixture({ version: 2 }),
          },
          capture: () => {
            posted = true;
          },
        },
      },
    );

    const block = await screen.findByTestId("agreements-to-discuss");
    expect(within(block).getByTestId("to-discuss-row-prop-1")).toHaveTextContent(
      "Andreas proposed adding to Money:",
    );
    expect(block).toHaveTextContent("No solo spend over $200");
    expect(block).toHaveTextContent("I want to talk about the number");
    // The parked list is the whole of this block (decision 7): a pending change
    // belongs on the Agreements page, not on a page about the next retro.
    expect(within(block).queryByTestId("to-discuss-row-prop-2")).not.toBeInTheDocument();
    // A reminder, not a second editing surface.
    expect(within(block).queryByRole("button", { name: "Discuss" })).not.toBeInTheDocument();
    expect(within(block).queryByRole("button", { name: "Withdraw" })).not.toBeInTheDocument();

    fireEvent.click(within(block).getByRole("button", { name: "Agree" }));
    await waitFor(() => expect(posted).toBe(true));
  });

  it("renders nothing at all when the household has parked nothing", async () => {
    renderBlock(documentFixture({ proposals: [proposalFixture({ status: "pending" })] }));
    await expect(screen.findByTestId("agreements-to-discuss")).rejects.toThrow();
  });

  it("says one muted line when the document fails to load, never an alert", async () => {
    stubFetchRoutes({
      [`GET ${DOC_URL}`]: { status: 500, body: { error: { code: "INTERNAL", message: "boom" } } },
    });
    renderWithRouter(<AgreementsToDiscuss />);

    const line = await screen.findByTestId("agreements-to-discuss-error");
    expect(line).toHaveTextContent("Couldn't load anything parked for this retro.");
    // This block is a guest on someone else's page: silence reads as having
    // nothing to show, and a red alert claims the Retros page itself broke.
    expect(line).not.toHaveAttribute("role", "alert");
  });

  it("a locked household still sees the parked row, with no Agree button", async () => {
    renderBlock(
      documentFixture({
        locked: true,
        owners: [OWNERS[0]],
        proposals: [parkedFixture({ canAgree: false, canWithdraw: false })],
      }),
    );

    const block = await screen.findByTestId("agreements-to-discuss");
    expect(block).toHaveTextContent("No solo spend over $200");
    expect(within(block).queryByRole("button", { name: "Agree" })).not.toBeInTheDocument();
  });

  // The composition gap the whole-branch review found: ProposalCard.tsx
  // disables Agree on targetChanged and explains why (decision 14); this
  // block never read that field at all, so a parked proposal whose target had
  // moved rendered a live, enabled Agree that could only ever answer 409
  // AGREEMENT_CHANGED. Same three sentences as ProposalCard.test.tsx's own
  // "addresses a stale proposal to whoever is reading", because decision 14
  // names exactly three and both surfaces must agree on all of them.
  it.each([
    [{ canWithdraw: true }, "Withdraw it and propose the change again."],
    [
      { canWithdraw: false, proposedByName: "Andreas" },
      "Ask Andreas to withdraw it and propose it again against the current wording.",
    ],
    [
      { canWithdraw: false, proposedByName: "" },
      "This needs withdrawing and proposing again against the current wording.",
    ],
  ] as [Partial<AgreementProposal>, string][])(
    "disables Agree on a stale parked proposal and names why (%#)",
    async (overrides, sentence) => {
      renderBlock(
        documentFixture({
          proposals: [parkedFixture({ targetChanged: true, canAgree: true, ...overrides })],
        }),
      );

      const block = await screen.findByTestId("agreements-to-discuss");
      expect(within(block).getByTestId("to-discuss-stale-note")).toHaveTextContent(sentence);
      expect(within(block).getByRole("button", { name: "Agree" })).toBeDisabled();
    },
  );

  it("shows the write failure in place and keeps the row, rather than emptying the block", async () => {
    renderBlock(documentFixture({ proposals: [parkedFixture()] }), {
      [AGREE_URL]: {
        status: 500,
        body: { error: { code: "INTERNAL", message: "boom" } },
      },
    });

    const block = await screen.findByTestId("agreements-to-discuss");
    fireEvent.click(within(block).getByRole("button", { name: "Agree" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't agree that just now.");
    expect(screen.getByTestId("to-discuss-row-prop-1")).toBeInTheDocument();
  });
});

// The spec's Frontend testing paragraph asks for two things no earlier task can
// express, because until now only one consumer of the key existed: "assert
// agree's TWO call sites separately", and "both mounted components re-rendering
// off that ONE invalidation". Mounting both under one renderWithRouter gives
// them a single QueryClient, which is exactly the production arrangement.
describe("the Agreements page and the To-discuss block, sharing one query key", () => {
  const parkedDoc = documentFixture({
    sections: [{ id: "sec-1", name: "Money", count: 0, visible: false, agreements: [] }],
    proposals: [parkedFixture()],
  });
  // What the same GET answers after the Agree lands: the proposal is gone from
  // `proposals` (it is accepted, and the document carries pending and parked
  // only) and the agreement it created is live in its section, which makes the
  // section visible for the first time.
  const agreedDoc = documentFixture({
    version: 2,
    updatedAt: "2026-09-05T10:00:00+08:00",
    sections: [
      {
        id: "sec-1", name: "Money", count: 1, visible: true,
        agreements: [{ id: "agr-1", number: 1, body: "No solo spend over $200" }],
      },
    ],
    proposals: [],
  });

  function renderBoth(agreeCapture?: () => void) {
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture() },
      [`GET ${DOC_URL}`]: [
        { status: 200, body: { agreements: parkedDoc } },
        { status: 200, body: { agreements: agreedDoc } },
      ],
      [AGREE_URL]: {
        status: 200,
        body: {
          proposal: proposalFixture({ status: "accepted", awaitingNames: [], canAgree: false }),
          agreements: agreedDoc,
        },
        ...(agreeCapture === undefined ? {} : { capture: agreeCapture }),
      },
    });
    const view = renderWithRouter(
      <>
        <AgreementsPage />
        <AgreementsToDiscuss />
      </>,
    );
    const documentGets = () =>
      fetchMock.mock.calls.filter(
        ([input, init]) => String(input) === DOC_URL && (init?.method ?? "GET") === "GET",
      ).length;
    return { ...view, fetchMock, documentGets };
  }

  it("one Agree from the block refreshes both screens off one refetch", async () => {
    const { documentGets } = renderBoth();

    const block = await screen.findByTestId("agreements-to-discuss");
    // Two consumers, one key, one request: the block does not fetch its own copy.
    expect(documentGets()).toBe(1);
    // The section is invisible while it holds nothing live (decision 8), so its
    // name appearing later is the page having re-rendered off the new document.
    expect(screen.queryByText("Money")).not.toBeInTheDocument();

    fireEvent.click(within(block).getByRole("button", { name: "Agree" }));

    // The page re-rendered: the section it could not show before is now there.
    expect(await screen.findByText("Money")).toBeInTheDocument();
    // The block re-rendered: nothing is parked any more, so it renders nothing.
    await waitFor(() =>
      expect(screen.queryByTestId("agreements-to-discuss")).not.toBeInTheDocument(),
    );
    // Two GETs, not three: the write invalidated one key and both components
    // re-rendered off the single refetch it caused.
    expect(documentGets()).toBe(2);
  });

  it("agree has two call sites, and the page's own card posts the same write", async () => {
    let posted = 0;
    const { documentGets } = renderBoth(() => {
      posted += 1;
    });

    const block = await screen.findByTestId("agreements-to-discuss");
    const agreeButtons = screen.getAllByRole("button", { name: "Agree" });
    // One on the page's ProposalCard, one in the block. If this is 1, a
    // mutation the hook returns has no caller on one of the two screens --
    // Goals shipped archive-and-restore exactly that way.
    expect(agreeButtons).toHaveLength(2);

    // Identified by NOT being inside the block, so this test does not depend on
    // Task 12's internal test ids.
    const pageAgree = agreeButtons.find((button) => !block.contains(button));
    expect(pageAgree).toBeDefined();

    fireEvent.click(pageAgree!);
    await waitFor(() => expect(posted).toBe(1));
    await waitFor(() => expect(documentGets()).toBe(2));
  });
});
