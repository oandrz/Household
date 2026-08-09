// Unit tests for the Subscriptions panel -- this component takes `bills`
// (every LIVE bill, MonthlyContributionsCard.test.tsx's own precedent: the
// card does its own isSubscription filter, the page only ever filters out
// archived) plus the server's own subscriptionsMonthlyMinor/
// subscriptionsAnnualMinor as props and does no fetching or arithmetic of
// its own, so a plain `render` is enough -- no router, no
// QueryClientProvider, no stubbed fetch.
//
// Literal strings are asserted throughout, not BILL_COPY's own exports --
// BillsPage.test.tsx's own convention: importing the copy module here would
// make an assertion tautological against a typo in that same module.
//
// The four behaviours below are the task brief's own numbered list:
// 1. heading carries the monthly total, the annual figure sits beneath.
// 2. a quarterly/yearly row shows its own amount and cadence, and the panel
//    says the totals are monthly equivalents.
// 3. no "last reviewed" line, anywhere, in any state.
// 4. an empty state explaining how a bill becomes a subscription.
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { SubscriptionsCard } from "./SubscriptionsCard";
import type { Bill } from "./billSchemas";

function billFixture(overrides: Partial<Bill> = {}): Bill {
  return {
    id: "bill-1",
    name: "Netflix",
    amountMinor: 1998,
    currency: "SGD",
    cadence: "monthly",
    nextDue: "2026-08-15",
    categoryId: "cat-1",
    categoryName: "Streaming",
    payFromAccountId: "acct-1",
    accountName: "DBS",
    paidByMembershipId: "membership-1",
    autopay: true,
    isSubscription: true,
    overdue: false,
    dueSoon: true,
    settled: false,
    archivedAt: null,
    ...overrides,
  };
}

const SYMBOLS: Record<string, string> = { SGD: "S$" };
const symbolFor = (currency: string) => SYMBOLS[currency];

function renderCard(
  bills: Bill[],
  props: Partial<{ currency: string; monthlyMinor: number; annualMinor: number }> = {},
) {
  render(
    <SubscriptionsCard
      bills={bills}
      currency={props.currency ?? "SGD"}
      symbolFor={symbolFor}
      monthlyMinor={props.monthlyMinor ?? 0}
      annualMinor={props.annualMinor ?? 0}
    />,
  );
}

describe("SubscriptionsCard", () => {
  it("lists the ticked bills with the monthly total in the heading and the annual figure beneath", () => {
    // monthlyMinor/annualMinor deliberately disagree with any sum a naive
    // client-side recompute of the one $19.98 bill below would produce --
    // if this component ever starts summing bills itself instead of
    // trusting these two props, this is the assertion that catches it.
    renderCard([billFixture({ id: "b1", name: "Netflix", amountMinor: 1998 })], {
      monthlyMinor: 7090,
      annualMinor: 85080,
    });

    expect(screen.getByTestId("subscriptions-heading")).toHaveTextContent("Subscriptions · S$70.90/mo");
    expect(screen.getByTestId("subscriptions-annual")).toHaveTextContent("S$850.80/year");

    const row = screen.getByTestId("subscription-row");
    expect(within(row).getByText("Netflix")).toBeInTheDocument();
    expect(within(row).getByText("S$19.98")).toBeInTheDocument();
  });

  it("a quarterly subscription renders its own amount and cadence, and the panel says the totals are monthly equivalents", () => {
    renderCard(
      [
        billFixture({
          id: "b1",
          name: "Insurance",
          amountMinor: 12000,
          cadence: "quarterly",
          categoryName: "Insurance",
        }),
      ],
      { monthlyMinor: 4000, annualMinor: 48000 },
    );

    const row = screen.getByTestId("subscription-row");
    // The row shows what is actually charged -- S$120 every quarter -- not
    // a monthly-equivalent S$40 that would silently disagree with the
    // amount on the bill's own record.
    expect(within(row).getByText("Insurance")).toBeInTheDocument();
    expect(within(row).getByText("S$120.00")).toBeInTheDocument();
    expect(within(row).getByText("Quarterly")).toBeInTheDocument();

    expect(screen.getByTestId("subscriptions-equivalent-note")).toHaveTextContent(
      "Totals above are monthly equivalents",
    );
  });

  it("a yearly subscription renders its own amount and cadence too, not just quarterly", () => {
    renderCard(
      [billFixture({ id: "b1", name: "Domain renewal", amountMinor: 3600, cadence: "yearly" })],
      { monthlyMinor: 300, annualMinor: 3600 },
    );

    const row = screen.getByTestId("subscription-row");
    expect(within(row).getByText("S$36.00")).toBeInTheDocument();
    expect(within(row).getByText("Yearly")).toBeInTheDocument();
  });

  it("a monthly subscription's row carries no cadence label -- the heading's own cadence is already monthly", () => {
    renderCard([billFixture({ id: "b1", name: "Netflix", cadence: "monthly" })], {
      monthlyMinor: 1998,
      annualMinor: 23976,
    });

    const row = screen.getByTestId("subscription-row");
    expect(within(row).queryByText("Monthly")).not.toBeInTheDocument();
  });

  it('renders no "last reviewed" line -- nothing in this product can set that date', () => {
    renderCard(
      [
        billFixture({ id: "b1", name: "Netflix" }),
        billFixture({ id: "b2", name: "Insurance", cadence: "quarterly", amountMinor: 12000 }),
      ],
      { monthlyMinor: 5998, annualMinor: 71976 },
    );

    expect(screen.queryByText(/reviewed/i)).not.toBeInTheDocument();
  });

  it("explains how a bill becomes a subscription when nothing is ticked", () => {
    renderCard([billFixture({ isSubscription: false })]);

    expect(screen.queryByTestId("subscription-row")).not.toBeInTheDocument();
    expect(screen.queryByTestId("subscriptions-annual")).not.toBeInTheDocument();
    const empty = screen.getByTestId("subscriptions-empty");
    // Names the exact control (the modal's own checkbox label) that turns
    // this state into the populated one -- not just "nothing here yet".
    expect(empty).toHaveTextContent('Tick "Counts as a subscription"');
  });

  it("an archived bill that is ticked as a subscription is never counted or rendered", () => {
    renderCard([
      billFixture({ id: "b1", isSubscription: true, archivedAt: "2026-06-01T00:00:00Z" }),
    ]);

    expect(screen.queryByTestId("subscription-row")).not.toBeInTheDocument();
    expect(screen.getByTestId("subscriptions-empty")).toBeInTheDocument();
  });
});
