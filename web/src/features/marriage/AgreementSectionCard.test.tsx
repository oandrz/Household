// Props in, nothing else: no router, no network -- PillarCard.test.tsx's shape.
// The fixture is local because the shared ones live in AgreementsPage.test.tsx,
// and importing from another test file would re-run that file's suite.
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AgreementSectionCard } from "./AgreementSectionCard";
import type { AgreementSection } from "./agreementSchemas";

function sectionFixture(name: string, first: number, bodies: string[]): AgreementSection {
  return {
    id: `sec-${name}`,
    name,
    count: bodies.length,
    visible: bodies.length > 0,
    agreements: bodies.map((body, i) => ({ id: `${name}-${i}`, number: first + i, body })),
  };
}

describe("AgreementSectionCard", () => {
  it("renders the section name, its count and every row in the order the server sent", () => {
    render(
      <AgreementSectionCard
        section={sectionFixture("Money", 3, [
          "Any purchase over S$500 gets discussed first.",
          "We review the budget on the first Sunday.",
          "Neither of us lends money without telling the other.",
        ])}
      />,
    );

    expect(screen.getByRole("heading", { level: 3, name: "Money" })).toBeInTheDocument();
    expect(screen.getByText("3 agreements")).toBeInTheDocument();
    const rows = screen.getAllByTestId(/^agreement-row-/);
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("03");
    expect(rows[0]).toHaveTextContent("Any purchase over S$500 gets discussed first.");
    expect(rows[2]).toHaveTextContent("05");
  });

  // The service composes the integer for the whole document on every read
  // (decisions 10 and 11) and the browser owns the padding, so this pins the
  // padding and nothing else: a card that derived its own numbers would
  // restart every section at 01.
  it("pads a single-digit number to two digits and leaves a two-digit one alone", () => {
    render(<AgreementSectionCard section={sectionFixture("Us", 9, ["A weekly walk", "One night out a month"])} />);

    expect(within(screen.getByTestId("agreement-row-Us-0")).getByText("09")).toBeInTheDocument();
    expect(within(screen.getByTestId("agreement-row-Us-1")).getByText("10")).toBeInTheDocument();
  });

  it("says 1 agreement, not 1 agreements", () => {
    render(<AgreementSectionCard section={sectionFixture("Conflict", 1, ["No raised voices in front of the kids."])} />);

    expect(screen.getByText("1 agreement")).toBeInTheDocument();
    expect(screen.queryByText("1 agreements")).not.toBeInTheDocument();
  });
});
