// Unit tests for the Recurring bills card. Every figure arrives computed by
// the server, so a plain `render` is enough. Literal strings are asserted,
// not BILL_COPY's exports.
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { RecurringBillsCard } from "./RecurringBillsCard";
import type { Bill, BillsSummary, RecurringCategory } from "./billSchemas";

const SYMBOLS: Record<string, string> = { SGD: "S$" };
const symbolFor = (currency: string) => SYMBOLS[currency];

function row(overrides: Partial<RecurringCategory> = {}): RecurringCategory {
  return {
    categoryId: "cat-utilities",
    categoryName: "Utilities",
    annualMinor: 216000,
    monthlyMinor: 18000,
    excludedNoRate: 0,
    ...overrides,
  };
}

function summary(overrides: Partial<BillsSummary> = {}): BillsSummary {
  return {
    currency: "SGD",
    dueThisMonthMinor: 0,
    paidSoFarMinor: 0,
    nextDue: null,
    autopayCount: 0,
    billCount: 0,
    subscriptionsMonthlyMinor: 0,
    subscriptionsAnnualMinor: 0,
    recurringMonthlyMinor: 0,
    recurringAnnualMinor: 0,
    recurringByCategory: [],
    excludedNoRate: 0,
    ...overrides,
  };
}

function netflix(): Bill {
  return {
    id: "bill-netflix",
    name: "Netflix",
    amountMinor: 1998,
    currency: "SGD",
    cadence: "monthly",
    nextDue: "2026-08-15",
    categoryId: "cat-streaming",
    categoryName: "Streaming",
    payFromAccountId: "acct-1",
    accountName: "DBS",
    paidByMembershipId: "",
    autopay: true,
    isSubscription: true,
    overdue: false,
    dueSoon: true,
    settled: false,
    archivedAt: null,
  };
}

function renderCard(s: BillsSummary, bills: Bill[] = []) {
  render(<RecurringBillsCard bills={bills} summary={s} symbolFor={symbolFor} />);
}

describe("RecurringBillsCard", () => {
  it("shows the monthly and yearly total of every repeating bill", () => {
    renderCard(
      summary({
        recurringMonthlyMinor: 64000,
        recurringAnnualMinor: 768000,
        recurringByCategory: [row({ monthlyMinor: 64000, annualMinor: 768000 })],
      }),
    );

    expect(screen.getByTestId("recurring-heading")).toHaveTextContent("Recurring bills");
    expect(screen.getByTestId("recurring-monthly")).toHaveTextContent("S$640.00/mo");
    expect(screen.getByTestId("recurring-annual")).toHaveTextContent("S$7,680.00/year");
  });

  it("lists one row per category in the order the server sends, naming the row with no category", () => {
    renderCard(
      summary({
        recurringMonthlyMinor: 47000,
        recurringAnnualMinor: 564000,
        recurringByCategory: [
          row({ categoryId: "cat-housing", categoryName: "Housing", monthlyMinor: 27000 }),
          row({ categoryId: "cat-utilities", categoryName: "Utilities", monthlyMinor: 18000 }),
          row({ categoryId: "", categoryName: "", monthlyMinor: 2000 }),
        ],
      }),
    );

    const rows = screen.getAllByTestId("recurring-row");
    expect(rows).toHaveLength(3);
    expect(within(rows[0]).getByText("Housing")).toBeInTheDocument();
    expect(within(rows[0]).getByText("S$270.00")).toBeInTheDocument();
    expect(within(rows[1]).getByText("Utilities")).toBeInTheDocument();
    expect(within(rows[2]).getByText("No category")).toBeInTheDocument();
    expect(within(rows[2]).getByText("S$20.00")).toBeInTheDocument();
    expect(screen.getByTestId("recurring-equivalent-note")).toHaveTextContent(
      "a yearly bill counts as 1/12 of its amount each month",
    );
  });

  it("says when a category's figure is short because a bill has no exchange rate", () => {
    renderCard(
      summary({
        recurringMonthlyMinor: 18000,
        recurringAnnualMinor: 216000,
        recurringByCategory: [
          row(),
          row({ categoryId: "cat-family", categoryName: "Family", monthlyMinor: 0, annualMinor: 0, excludedNoRate: 2 }),
        ],
      }),
    );

    const family = screen.getAllByTestId("recurring-row")[1];
    expect(within(family).getByText("2 bills not counted: no exchange rate")).toBeInTheDocument();
    // No figure rather than S$0.00, which would read as "costs nothing".
    expect(within(family).queryByText("S$0.00")).not.toBeInTheDocument();
    expect(within(family).getByText("—")).toBeInTheDocument();
    expect(within(screen.getAllByTestId("recurring-row")[0]).queryByText(/not counted/)).not.toBeInTheDocument();
  });

  // Value: protects=a partly short category shows its counted figure and the singular "1 bill not counted" note together;
  // fails_when=the dash rule drops its annualMinor === 0 half, hiding real money, or the count loses its singular;
  // why_new=the test above covers only a row with nothing counted; seam=none;
  // no_claim=server figures (props stand in for the API; jsdom render only)
  it("keeps a category's figure when only some of its bills have no exchange rate, and says how many are missing", () => {
    renderCard(
      summary({
        recurringMonthlyMinor: 18000,
        recurringAnnualMinor: 216000,
        recurringByCategory: [row({ categoryId: "cat-family", categoryName: "Family", excludedNoRate: 1 })],
      }),
    );

    const family = screen.getByTestId("recurring-row");
    expect(within(family).getByText("S$180.00")).toBeInTheDocument();
    expect(within(family).getByText("1 bill not counted: no exchange rate")).toBeInTheDocument();
    expect(within(family).queryByText("—")).not.toBeInTheDocument();
  });

  it("explains an empty total instead of showing S$0.00, and leaves the subscriptions section out", () => {
    renderCard(summary());

    expect(screen.getByTestId("recurring-empty")).toHaveTextContent("No repeating bills yet. One-offs aren't counted.");
    expect(screen.queryByText("S$0.00")).not.toBeInTheDocument();
    expect(screen.queryByTestId("subscriptions-section")).not.toBeInTheDocument();
  });

  it("holds the subscriptions as a section inside the recurring total", () => {
    renderCard(
      summary({
        recurringMonthlyMinor: 19998,
        recurringAnnualMinor: 239976,
        recurringByCategory: [row({ monthlyMinor: 18000 }), row({ categoryId: "cat-streaming", categoryName: "Streaming", monthlyMinor: 1998 })],
        subscriptionsMonthlyMinor: 1998,
        subscriptionsAnnualMinor: 23976,
      }),
      [netflix()],
    );

    const card = screen.getByTestId("recurring-bills-card");
    const section = within(card).getByTestId("subscriptions-section");
    expect(within(section).getByTestId("subscriptions-heading")).toHaveTextContent("Of which subscriptions · S$19.98/mo");
    expect(within(section).getByText("Netflix")).toBeInTheDocument();
  });
});
