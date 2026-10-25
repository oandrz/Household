import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { formatMoney } from "./formatMoney";
import { CashflowMonthDetail } from "./CashflowMonthDetail";
import type { CashflowMonth, CashflowReconciliation, CashflowSumLine } from "./cashflowSchemas";

const formatAmount = (minor: number) => formatMoney(minor, "SGD", "S$");

function line(overrides: Partial<CashflowSumLine>): CashflowSumLine {
  return { kind: "loan", name: "", amountMinor: 0, paidOn: "2026-09-01", shareIndex: 0, spread: 0, ...overrides };
}

const wholeBill = line({ kind: "bill_in_full", name: "Car insurance", amountMinor: -210000, paidOn: "2026-09-20", spread: 12 });
const septemberShare = line({ kind: "bill_share", name: "Car insurance", amountMinor: 17500, paidOn: "2026-09-20", shareIndex: 1, spread: 12 });
const carLoan = line({ kind: "loan", name: "Car loan", amountMinor: 80000, paidOn: "2026-09-28" });

function reconciliation(overrides: Partial<CashflowReconciliation> = {}): CashflowReconciliation {
  return {
    status: "shown",
    spentMinor: 519450,
    lines: [wholeBill, septemberShare, carLoan],
    unratedCount: 0,
    unratedEarlierBill: false,
    unratedCurrencies: [],
    ...overrides,
  };
}

function monthFixture(overrides: Partial<CashflowMonth> = {}): CashflowMonth {
  return {
    month: "2026-09",
    status: "solid",
    inMinor: 600000,
    outMinor: 406950,
    leftMinor: 193050,
    keptBasisPoints: 3000,
    spreadMinor: 17500,
    loanMinor: 80000,
    transactionCount: 9,
    grey: [],
    unconvertibleCurrencies: [],
    projectedLeftMinor: null,
    confirmation: null,
    contributions: [],
    reconciliation: reconciliation(),
    ...overrides,
  };
}

function open(month: CashflowMonth) {
  renderWithRouter(<CashflowMonthDetail month={month} formatAmount={formatAmount} onClose={() => {}} />);
  return screen.findByTestId("cashflow-month-detail");
}

// Each row's label and amount, in the order they appear.
function rows(detail: HTMLElement) {
  return within(detail)
    .getAllByRole("listitem")
    .map((item) => [
      item.querySelector("[data-part='label']")?.textContent,
      item.querySelector("[data-part='amount']")?.textContent,
    ]);
}

describe("CashflowMonthDetail worked sum", () => {
  it("shows September's sum in order with signs on the amounts only", async () => {
    const detail = await open(monthFixture());
    expect(within(detail).getByRole("heading", { name: "How September's money out adds up" })).toBeInTheDocument();
    expect(rows(detail)).toEqual([
      ["Spent in September", "S$5,194.50"],
      ["Car insurance, whole yearly bill", "−S$2,100.00"],
      ["Car insurance, September's share", "+S$175.00"],
      ["Car loan repayment", "+S$800.00"],
      ["= Money out", "S$4,069.50"],
    ]);
    for (const [label] of rows(detail)) expect(label).not.toMatch(/^[+−]/);
  });

  it("explains each line in grey under its label", async () => {
    const detail = await open(monthFixture());
    expect(within(detail).getByText("From the Transactions page")).toBeInTheDocument();
    expect(within(detail).getByText("Paid 20 Sep 2026, taken out here and spread over 12 months")).toBeInTheDocument();
    expect(within(detail).getByText("Month 1 of 12")).toBeInTheDocument();
    expect(within(detail).getByText("Transfer to a loan account, 28 Sep 2026")).toBeInTheDocument();
  });

  it("says Money out so far for the month still running", async () => {
    const detail = await open(monthFixture({ status: "current" }));
    expect(rows(detail).at(-1)).toEqual(["= Money out so far", "S$4,069.50"]);
  });

  it("uses the earlier-bill note for a share of a bill paid in another month", async () => {
    const earlier = line({ kind: "bill_share", name: "Car insurance", amountMinor: 17500, paidOn: "2026-09-20", shareIndex: 2, spread: 12 });
    const detail = await open(
      monthFixture({
        month: "2026-10",
        outMinor: 17500,
        reconciliation: reconciliation({ spentMinor: 0, lines: [earlier] }),
      }),
    );
    expect(within(detail).getByText("Month 2 of 12 of the yearly bill paid 20 Sep 2026")).toBeInTheDocument();
    expect(within(detail).getByText("Car insurance, October's share")).toBeInTheDocument();
  });

  it("calls a three-month bill quarterly", async () => {
    const quarterly = line({ kind: "bill_in_full", name: "Water", amountMinor: -9000, paidOn: "2026-09-02", spread: 3 });
    const detail = await open(monthFixture({ reconciliation: reconciliation({ lines: [quarterly] }) }));
    expect(within(detail).getByText("Water, whole quarterly bill")).toBeInTheDocument();
    expect(within(detail).getByText("Paid 2 Sep 2026, taken out here and spread over 3 months")).toBeInTheDocument();
  });

  it("keeps the same shape when there is nothing to explain", async () => {
    const detail = await open(
      monthFixture({ outMinor: 519450, reconciliation: reconciliation({ lines: [] }) }),
    );
    expect(rows(detail)).toEqual([
      ["Spent in September", "S$5,194.50"],
      ["= Money out", "S$5,194.50"],
    ]);
    expect(within(detail).getByText("No yearly bills spread and no loan repayments in this month.")).toBeInTheDocument();
  });

  const hiddenSums: Array<[string, Partial<CashflowReconciliation>, string]> = [
    [
      "a transaction",
      { status: "no_rate", lines: [], unratedCount: 1, unratedCurrencies: ["USD"] },
      "Hearth can't add up September: 1 transaction is not counted because Hearth has no exchange rate for USD.",
    ],
    [
      "an earlier bill",
      { status: "no_rate", lines: [], unratedEarlierBill: true, unratedCurrencies: ["USD", "EUR"] },
      "Hearth can't add up September: a bill spread into this month is in USD and EUR, and Hearth has no exchange rate for it.",
    ],
    [
      "both",
      { status: "no_rate", lines: [], unratedCount: 2, unratedEarlierBill: true, unratedCurrencies: ["USD", "EUR", "GBP"] },
      "Hearth can't add up September: it has no exchange rate for USD, EUR and GBP, so 2 transactions and a bill spread into this month are not counted.",
    ],
  ];

  it.each(hiddenSums)("hides the sum when a rate is missing for %s", async (_name, overrides, sentence) => {
    const detail = await open(monthFixture({ reconciliation: reconciliation(overrides) }));
    expect(within(detail).getByText(sentence)).toBeInTheDocument();
    expect(within(detail).queryByText(/= Money out/)).not.toBeInTheDocument();
    expect(within(detail).queryAllByRole("listitem")).toHaveLength(0);
  });

  const mismatchSentence = "Hearth can't show how September adds up right now. The figures above are still the ones it uses.";

  it("hides the sum on a mismatch", async () => {
    const detail = await open(monthFixture({ reconciliation: reconciliation({ status: "mismatch" }) }));
    expect(within(detail).getByText(mismatchSentence)).toBeInTheDocument();
    expect(within(detail).queryByText(/= Money out/)).not.toBeInTheDocument();
  });

  it("hides the sum on an unknown spread", async () => {
    const odd = line({ kind: "bill_in_full", name: "Odd", amountMinor: -5000, paidOn: "2026-09-02", spread: 6 });
    const detail = await open(monthFixture({ reconciliation: reconciliation({ lines: [odd] }) }));
    expect(within(detail).getByText(mismatchSentence)).toBeInTheDocument();
    expect(within(detail).queryByText("Odd, whole 6 bill")).not.toBeInTheDocument();
    expect(within(detail).queryByText(/= Money out/)).not.toBeInTheDocument();
  });

  it("shows only the link when there is no sum to show", async () => {
    const detail = await open(monthFixture({ reconciliation: null }));
    expect(within(detail).queryByText(/= Money out/)).not.toBeInTheDocument();
    expect(within(detail).getByRole("link", { name: "See September's 9 transactions" })).toBeInTheDocument();
  });

  it("names the count in the link and keeps the month in it", async () => {
    const detail = await open(monthFixture());
    const link = within(detail).getByRole("link", { name: "See September's 9 transactions" });
    expect(link.getAttribute("href")).toMatch(/\/money\/transactions\?month=2026-09$/);
  });
});
