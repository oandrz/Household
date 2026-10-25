// Small wording and layout rules on the Cashflow page.
import { fireEvent, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { CashflowPage } from "./CashflowPage";
import { keptCell, keptLine, lastFinishedLabel } from "./cashflowCopy";
import type { CashflowMonth, CashflowPage as CashflowData } from "./cashflowSchemas";

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

function monthFixture(overrides: Partial<CashflowMonth> = {}): CashflowMonth {
  return {
    month: "2026-09", status: "solid", inMinor: 500000, outMinor: 400000, leftMinor: 100000,
    keptBasisPoints: 2000, spreadMinor: 0, loanMinor: 0, transactionCount: 12, grey: [],
    unconvertibleCurrencies: [], projectedLeftMinor: null, confirmation: null,
    contributions: [], reconciliation: null,
    ...overrides,
  };
}

function pageFixture(oldestYear: number): CashflowData {
  const september = monthFixture();
  return {
    currency: "SGD",
    hasTransactions: true,
    oldestYear,
    light: { colour: "green", reasons: [] },
    average: { months: 3, inMinor: 500000, outMinor: 450000, leftMinor: 50000, keptBasisPoints: 1000 },
    cushion: { known: true, tenths: 42, reason: "" },
    lastFinished: september,
    current: monthFixture({ month: "2026-10", status: "current" }),
    recent: [september],
    year: {
      year: 2026, months: [september], keptSoFarMinor: 900000, incomeMinor: 4500000, greyMonths: 0,
      keptBasisPoints: 2000, sameMonthsLastYearMinor: 700000, headingForMinor: 1100000,
      billsToCome: [
        { billId: "bill-1", name: "Car insurance", amountMinor: 210000, nextDue: "2026-12-01", unconvertible: false },
      ],
    },
  } as CashflowData;
}

function renderPage(oldestYear: number, overrides: Partial<CashflowData> = {}) {
  stubFetchRoutes({
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/cashflow": { status: 200, body: { ...pageFixture(oldestYear), ...overrides } },
  });
  renderWithRouter(<CashflowPage />);
}

async function openYearly() {
  fireEvent.click(await screen.findByRole("tab", { name: "Yearly" }));
  await screen.findByText("Kept so far");
}

afterEach(() => vi.unstubAllGlobals());

describe("keptCell", () => {
  it("always shows two decimals so a column of shares lines up", () => {
    expect(keptCell(2000)).toBe("20.00%");
    expect(keptCell(3758)).toBe("37.58%");
  });
  it("uses a plain hyphen for a negative share", () => expect(keptCell(-42)).toBe("-0.42%"));
  it("shows a dash when income was zero", () => expect(keptCell(null)).toBe("—"));
  it("leaves the sentence form short", () => expect(keptLine(2000)).toBe("20% of income kept"));
});

describe("last finished month heading", () => {
  it("names itself in words and reads as a heading", async () => {
    expect(lastFinishedLabel("2026-09")).toBe("Last finished month: September 2026");
    renderPage(2024);
    const heading = await screen.findByRole("heading", { name: "Last finished month: September 2026" });
    expect(heading).toHaveClass("text-[13px]", "font-semibold", "text-ink");
  });
});

describe("year picker", () => {
  it("hides the year arrows when there is only one year", async () => {
    renderPage(2026);
    await openYearly();
    expect(screen.getByTestId("cashflow-year")).toHaveTextContent("2026");
    expect(screen.queryByRole("button", { name: "Previous year" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Next year" })).not.toBeInTheDocument();
  });

  it("left-aligns the year label when there are no arrows, and centres it when there are", async () => {
    renderPage(2026);
    await openYearly();
    expect(screen.getByTestId("cashflow-year")).not.toHaveClass("text-center");
  });

  it("centres the year label between the arrows", async () => {
    renderPage(2024);
    await openYearly();
    expect(screen.getByTestId("cashflow-year")).toHaveClass("text-center");
  });

  it("shows both arrows when there is more than one year", async () => {
    renderPage(2024);
    await openYearly();
    expect(screen.getByRole("button", { name: "Previous year" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next year" })).toBeInTheDocument();
  });
});

describe("yearly cards", () => {
  it("the Kept so far card shows this year's share before last year's comparison", async () => {
    renderPage(2024);
    await openYearly();
    const share = screen.getByText("20% of income kept this year");
    const lastYear = screen.getByText(/in the same months last year/);
    expect(share.compareDocumentPosition(lastYear) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("lines up the amounts of bills still to come", async () => {
    renderPage(2024);
    await openYearly();
    const item = screen.getByText("Car insurance").closest("li") as HTMLElement;
    expect(within(item).getByText("S$2,100.00")).toHaveClass("tabular");
  });
});

describe("page semantics", () => {
  it("names the tab list", async () => {
    renderPage(2024);
    expect(await screen.findByRole("tablist", { name: "Cashflow view" })).toBeInTheDocument();
  });

  it("makes the empty-state headline a heading", async () => {
    renderPage(2024, { hasTransactions: false });
    expect(await screen.findByRole("heading", { name: "Nothing to show yet" })).toBeInTheDocument();
  });

  it("gives both tabs a border so selecting one does not change its size", async () => {
    renderPage(2024);
    const monthly = await screen.findByRole("tab", { name: "Monthly" });
    expect(monthly).toHaveClass("border", "border-accent");
    expect(screen.getByRole("tab", { name: "Yearly" })).toHaveClass("border");
  });
});

describe("open month", () => {
  it("shades the open month's row and detail together and flips its arrow", async () => {
    renderPage(2024);
    const button = await screen.findByRole("button", { name: "September 2026" });
    expect(button).toHaveAttribute("aria-expanded", "false");
    expect(button.querySelector("[aria-hidden='true']")).toHaveTextContent("▸");
    expect(button.closest("tr")).not.toHaveClass("bg-canvas");

    fireEvent.click(button);

    const open = screen.getByRole("button", { name: "September 2026" });
    expect(open.querySelector("[aria-hidden='true']")).toHaveTextContent("▾");
    expect(open.closest("tr")).toHaveClass("bg-canvas");
    const detailRow = screen.getByTestId("cashflow-month-detail").closest("tr");
    expect(detailRow).toHaveClass("bg-canvas");
  });
});
