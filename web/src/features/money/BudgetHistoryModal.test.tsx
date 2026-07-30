// Unit tests for the History modal's own math and rendering -- this
// component takes `months` as a prop and does no fetching of its own (that
// lives in useBudgetHistory.ts, exercised through BudgetPage.test.tsx's
// integration tests instead), so a plain `render` is enough: no router, no
// QueryClientProvider, no stubbed fetch.
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { BudgetHistoryModal } from "./BudgetHistoryModal";
import type { BudgetHistoryResponse } from "./budgetSchemas";

// One fixture exercises the gap-month, current-month and closed-month cases
// together: May is missing entirely (the server's own contract -- "a month
// without a budget row is simply absent, never zero-filled" -- so this is
// what a real gap looks like on the wire, not a zero row this component has
// to filter out itself). All three closed months share one budgeted figure
// (S$5,000) so the summary math below is easy to hand-check:
//   avg spend = (2000 + 6500 + 3500) / 3 = 4000.00
//   avg saved = (300000 + -150000 + 150000) / 3 = 1000.00  (300000/-150000/150000 in minor units)
//   under budget (spent <= budgeted): Jun yes, Apr no, Feb yes -> 2 of 3
const MONTHS: BudgetHistoryResponse["months"] = [
  { month: "2026-07", budgetedMinor: 500000, spentMinor: 300000, closed: false },
  { month: "2026-06", budgetedMinor: 500000, spentMinor: 200000, closed: true },
  // 2026-05 deliberately absent -- the gap.
  { month: "2026-04", budgetedMinor: 500000, spentMinor: 650000, closed: true },
  { month: "2026-02", budgetedMinor: 500000, spentMinor: 350000, closed: true },
];

function renderModal(months: BudgetHistoryResponse["months"] = MONTHS) {
  const onPickMonth = vi.fn();
  const onClose = vi.fn();
  render(
    <BudgetHistoryModal
      months={months}
      currency="SGD"
      symbol="S$"
      onPickMonth={onPickMonth}
      onClose={onClose}
    />,
  );
  return { onPickMonth, onClose };
}

describe("BudgetHistoryModal", () => {
  it("computes avg spend and avg saved over closed months only", () => {
    renderModal();

    expect(screen.getByText("Avg monthly spend").nextSibling).toHaveTextContent("S$4,000.00");
    expect(screen.getByText("Avg saved / month").nextSibling).toHaveTextContent("S$1,000.00");
  });

  it("counts months under budget from closed budgeted months only", () => {
    renderModal();

    expect(screen.getByText("Months under budget").nextSibling).toHaveTextContent("2 of 3");
  });

  // Pins the boundary decision BudgetHistoryModal.tsx's own comment states:
  // landing exactly on the cap counts as under, not over -- the same
  // direction BudgetStatCards.tsx treats a zero Remaining.
  it("counts a closed month that spent exactly its cap as under budget", () => {
    renderModal([{ month: "2026-06", budgetedMinor: 500000, spentMinor: 500000, closed: true }]);

    expect(screen.getByText("Months under budget").nextSibling).toHaveTextContent("1 of 1");
  });

  // A closed month with every cap removed (budgetedMinor 0) has nothing to
  // be judged "under" or "over" against, so it must not drag the averages
  // or the denominator -- excluded entirely, not treated as a 100%-over
  // month the way `barWidthPercent`'s own capMinor<=0 guard would read it.
  it("excludes a closed month with a zero budget total from every summary figure", () => {
    renderModal([
      { month: "2026-06", budgetedMinor: 500000, spentMinor: 400000, closed: true },
      { month: "2026-05", budgetedMinor: 0, spentMinor: 12000, closed: true },
    ]);

    expect(screen.getByText("Avg monthly spend").nextSibling).toHaveTextContent("S$4,000.00");
    expect(screen.getByText("Months under budget").nextSibling).toHaveTextContent("1 of 1");
  });

  it("renders no row for the gap month the server simply omitted", () => {
    renderModal();

    const rows = screen.getAllByTestId("budget-history-row");
    expect(rows).toHaveLength(4);
    expect(rows.some((row) => row.textContent?.includes("May"))).toBe(false);
  });

  it("marks the current month's row 'so far' instead of a signed result", () => {
    renderModal();

    const rows = screen.getAllByTestId("budget-history-row");
    const julyRow = rows.find((row) => row.textContent?.includes("Jul 2026"));
    expect(julyRow).toBeDefined();
    expect(within(julyRow!).getByTestId("budget-history-row-so-far")).toHaveTextContent("so far");
    // The spent figure still shows, marked with the design's own asterisk
    // for "still in progress", not a real signed result.
    expect(julyRow).toHaveTextContent("S$3,000.00*");
  });

  it("shows a signed, coloured result on closed rows -- under in the accent colour, over in the danger colour", () => {
    renderModal();

    const rows = screen.getAllByTestId("budget-history-row");
    const juneRow = rows.find((row) => row.textContent?.includes("Jun 2026"))!;
    const aprilRow = rows.find((row) => row.textContent?.includes("Apr 2026"))!;

    const juneResult = within(juneRow).getByTestId("budget-history-row-result");
    expect(juneResult).toHaveTextContent("−S$3,000.00");
    expect(juneResult.className).toContain("text-accent");

    const aprilResult = within(aprilRow).getByTestId("budget-history-row-result");
    expect(aprilResult).toHaveTextContent("+S$1,500.00");
    expect(aprilResult.className).toContain("text-danger");
  });

  it("caps an over-budget closed month's bar width at 100% visually", () => {
    renderModal();

    const rows = screen.getAllByTestId("budget-history-row");
    const aprilRow = rows.find((row) => row.textContent?.includes("Apr 2026"))!;
    const bar = aprilRow.querySelector<HTMLElement>(".bg-danger");
    expect(bar).not.toBeNull();
    expect(bar!.style.width).toBe("100%");
  });

  it("keeps the current month's bar the accent colour regardless of how much of the cap it has used", () => {
    renderModal([{ month: "2026-07", budgetedMinor: 500000, spentMinor: 900000, closed: false }]);

    const row = screen.getByTestId("budget-history-row");
    expect(row.querySelector(".bg-accent")).not.toBeNull();
    expect(row.querySelector(".bg-danger")).toBeNull();
  });

  it("calls onPickMonth with the clicked row's month, and does not call onClose itself", () => {
    const { onPickMonth, onClose } = renderModal();

    const rows = screen.getAllByTestId("budget-history-row");
    const juneRow = rows.find((row) => row.textContent?.includes("Jun 2026"))!;
    juneRow.click();

    expect(onPickMonth).toHaveBeenCalledWith("2026-06");
    // Closing on pick is BudgetPage's job (it owns the modal's open state);
    // this component only reports the pick.
    expect(onClose).not.toHaveBeenCalled();
  });

  // Spec decision, same class as Task 12's "rolls into" pin: Export CSV is
  // in the design's mockup but deferred, and must not ship even accidentally
  // -- see BUDGET_COPY, which never grows an `exportCsv` string to begin
  // with.
  it("never renders an Export CSV control", () => {
    renderModal();

    expect(screen.queryByText(/export csv/i)).not.toBeInTheDocument();
  });

  it("shows sensible empty copy, not a crash or a zero row, when there is no history yet", () => {
    renderModal([]);

    expect(screen.getByTestId("budget-history-empty")).toHaveTextContent(
      "No budget history yet. Once a month closes, it shows up here.",
    );
    expect(screen.queryAllByTestId("budget-history-row")).toHaveLength(0);
  });
});
