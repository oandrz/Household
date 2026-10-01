import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { BudgetByPerson } from "./BudgetByPerson";
import { BUDGET_COPY } from "./budgetCopy";

describe("BudgetByPerson", () => {
  it("says nobody has spent anything yet when the month has no spending", () => {
    render(<BudgetByPerson people={[]} currency="SGD" symbol="S$" />);

    expect(screen.getByText(BUDGET_COPY.byPersonEmpty)).toBeInTheDocument();
    expect(screen.queryAllByTestId("budget-person-row")).toHaveLength(0);
  });

  it("shows one row per person and no empty message once someone has spent", () => {
    render(
      <BudgetByPerson
        people={[{ membershipId: "m-1", name: "Andreas", spentMinor: 2468 }]}
        currency="SGD"
        symbol="S$"
      />,
    );

    expect(screen.getAllByTestId("budget-person-row")).toHaveLength(1);
    expect(screen.queryByText(BUDGET_COPY.byPersonEmpty)).not.toBeInTheDocument();
  });
});
