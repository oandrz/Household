import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { AgreementsPage } from "./AgreementsPage";
import type { AgreementSection } from "./agreementSchemas";
import {
  DOC_URL,
  ONE_OWNER,
  documentFixture,
  emptyDoc,
  emptySection,
  proposalFixture,
  renderPage,
  seededDoc,
} from "./agreementFixtures";

afterEach(() => {
  vi.unstubAllGlobals();
});

function sectionFixture(name: string, first: number, bodies: string[]): AgreementSection {
  return {
    id: `sec-${name}`,
    name,
    count: bodies.length,
    visible: bodies.length > 0,
    agreements: bodies.map((body, i) => ({ id: `${name}-${i}`, number: first + i, body })),
  };
}
const headingsIn = (testId: string) =>
  within(screen.getByTestId(testId))
    .getAllByRole("heading", { level: 3 })
    .map((heading) => heading.textContent);

describe("AgreementsPage", () => {
  it("a limited member is told this is owner-only, not that something broke", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 403,
        body: { error: { code: "FORBIDDEN", message: "Owner only." } },
      },
    });

    expect(await screen.findByTestId("agreements-owner-only")).toHaveTextContent("Owner only");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("a non-403 failure renders the alert, not the owner-only explanation", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Broke." } },
      },
    });

    expect(await screen.findByTestId("agreements-load-error")).toHaveTextContent(
      "Couldn't load your agreements.",
    );
    expect(screen.queryByTestId("agreements-owner-only")).not.toBeInTheDocument();
  });

  // Decision 2. A household that has never had two owners has nothing to show
  // and no history to read, so all three header controls are absent -- there is
  // no version to look back at and nothing that could be proposed.
  it("a household that has never had two owners is offered the invite deep link and no write controls", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 200,
        body: emptyDoc({ locked: true, owners: ONE_OWNER }),
      },
    });

    expect(await screen.findByTestId("agreements-locked-invite")).toHaveTextContent(
      "This household has one owner.",
    );
    expect(screen.getByRole("link", { name: "Invite your partner" })).toHaveAttribute(
      "href",
      "/settings?invite=true",
    );
    expect(screen.queryByTestId("agreements-frozen")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agreements-propose")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agreements-new-section")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agreements-history")).not.toBeInTheDocument();
  });

  // Decision 3: the promise two people made does not stop existing when one of
  // them leaves. A live agreement proves this household once had two owners;
  // sections alone would not, being labels rather than promises (decision 8).
  //
  // The spec's own words for this state: "Version history stays, being a read;
  // + Section, Propose a change ... are GONE, not disabled" -- so this asserts
  // absence, which a disabled-button implementation would fail.
  it("a locked household with content keeps its document, names its frozen proposals, and keeps only Version history", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 200,
        body: {
          agreements: documentFixture({
            locked: true,
            owners: ONE_OWNER,
            version: 2,
            updatedAt: "2026-06-28T21:18:52+08:00",
            proposals: [proposalFixture()],
            sections: [
              {
                ...emptySection("Money"),
                count: 1,
                visible: true,
                agreements: [{ id: "a-1", number: 1, body: "One shared account for bills" }],
              },
            ],
          }),
        },
      },
    });

    expect(await screen.findByTestId("agreements-frozen")).toHaveTextContent(
      "1 change is waiting for a second owner",
    );
    expect(screen.queryByTestId("agreements-locked-invite")).not.toBeInTheDocument();
    expect(screen.getByTestId("agreements-subtitle")).toHaveTextContent("v2, updated 28 Jun");
    expect(screen.getByTestId("agreements-history")).toBeInTheDocument();
    expect(screen.queryByTestId("agreements-propose")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agreements-new-section")).not.toBeInTheDocument();
  });

  // The first render passes vacuously once the query has answered, so this
  // holds the response open: the only way to see what a cold load actually
  // paints. renderPage is deliberately NOT used -- this test replaces fetch
  // wholesale so that /auth/me hangs too.
  it("says nothing about a second owner while the query is still in flight", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>(() => {})),
    );

    renderWithRouter(<AgreementsPage />);

    expect(await screen.findByText("Loading…")).toBeInTheDocument();
    expect(screen.queryByTestId("agreements-locked-invite")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agreements-page")).not.toBeInTheDocument();
  });

  it("an unlocked household with no sections gets the empty state and all three header controls", async () => {
    renderPage({ [`GET ${DOC_URL}`]: { status: 200, body: emptyDoc() } });

    expect(await screen.findByTestId("agreements-empty")).toHaveTextContent(
      "Write your first agreements",
    );
    expect(screen.getByTestId("agreements-empty")).toHaveTextContent("Popular starting points");
    expect(screen.getByRole("button", { name: "+ Section" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Version history" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Propose a change" })).toBeInTheDocument();
    // The version clause is absent until updatedAt is a real moment -- never
    // "v1, updated —".
    expect(screen.getByTestId("agreements-subtitle")).not.toHaveTextContent("v1");
  });

  // Decision 8 makes an empty section invisible, so a naively rendered starter
  // set changes nothing on screen. This catches a button that appears to do
  // nothing -- browser criterion 4, pinned here as well because a walk that
  // navigates and waits would miss it too.
  it("Use starter set lands on the seeded state, which names the four sections", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: [
        { status: 200, body: emptyDoc() },
        { status: 200, body: seededDoc() },
      ],
      [`POST ${DOC_URL}/starter-set`]: { status: 200, body: seededDoc() },
    });

    fireEvent.click(await screen.findByRole("button", { name: "Use starter set" }));

    await waitFor(() => expect(screen.getByTestId("agreements-seeded")).toBeInTheDocument());
    expect(screen.getByTestId("agreements-seeded")).toHaveTextContent(
      "Money, Conflict, Home & kids and Us are ready.",
    );
    expect(screen.queryByRole("button", { name: "Use starter set" })).not.toBeInTheDocument();
  });

  // Column-MAJOR: the design's 01-12 runs continuously down one column and on
  // into the next, so Money 01-02 and Conflict 03-05 are the left column. The
  // row-major fill a grid-cols-2 produces would put Conflict beside Money and
  // zig-zag the numbering down the page. `visible` is the server's own flag
  // (decision 8), never a rule re-derived here.
  it("renders the visible sections in two column-major columns, skipping the ones the server hid", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 200,
        body: {
          agreements: documentFixture({
            sections: [
              sectionFixture("Money", 1, ["One shared account", "Any purchase over S$500 gets discussed first."]),
              sectionFixture("Conflict", 3, ["No raised voices", "No silent treatment", "We finish it the same day"]),
              sectionFixture("Home & kids", 6, ["Bedtime is a two-person job", "Saturday mornings are the kids'"]),
              sectionFixture("Us", 8, ["One night out a month"]),
              sectionFixture("Faith & values", 9, []),
            ],
          }),
        },
      },
    });

    await screen.findByTestId("agreements-column-0");
    expect(headingsIn("agreements-column-0")).toEqual(["Money", "Conflict"]);
    expect(headingsIn("agreements-column-1")).toEqual(["Home & kids", "Us"]);
    // An empty section stays invisible in the document (decision 8) while the
    // propose picker still offers it, which Task 13 reads off the same array.
    expect(screen.queryByText("Faith & values")).not.toBeInTheDocument();
    expect(screen.getByText("03")).toBeInTheDocument();
    expect(screen.getByText("3 agreements")).toBeInTheDocument();
  });

  // Decision 3: two owners can become one, and the promise those two people
  // made does not stop existing when one of them does. The document renders;
  // the banner explains; nothing here is a write.
  it("a locked household still sees its whole document under the frozen banner", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 200,
        body: {
          agreements: documentFixture({
            locked: true,
            owners: ONE_OWNER,
            version: 2,
            updatedAt: "2026-06-28T21:18:52+08:00",
            sections: [sectionFixture("Money", 1, ["One shared account", "Any purchase over S$500 gets discussed first."])],
          }),
        },
      },
    });

    expect(await screen.findByTestId("agreements-frozen")).toBeInTheDocument();
    expect(headingsIn("agreements-column-0")).toEqual(["Money"]);
    expect(screen.getByTestId("agreement-row-Money-0")).toHaveTextContent("One shared account");
  });
});
