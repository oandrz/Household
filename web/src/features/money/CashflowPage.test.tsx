// Drives the whole Cashflow page through its real components against a
// stubbed network, so what is asserted is what a person would read.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { CashflowPage } from "./CashflowPage";
import { cashflowCopy, cushionText, greyReasonText, reasonText } from "./cashflowCopy";
import type { CashflowMonth, CashflowPage as CashflowData, CashflowReason } from "./cashflowSchemas";
import { cashflowReasonCodeSchema } from "./cashflowSchemas";

const CURRENCIES = {
  status: 200,
  body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
};

function monthFixture(overrides: Partial<CashflowMonth> = {}): CashflowMonth {
  return {
    month: "2026-09",
    status: "solid",
    inMinor: 500000,
    outMinor: 400000,
    leftMinor: 100000,
    keptBasisPoints: 2000,
    spreadMinor: 0,
    loanMinor: 0,
    transactionCount: 12,
    grey: [],
    unconvertibleCurrencies: [],
    projectedLeftMinor: null,
    confirmation: null,
    contributions: [],
    ...overrides,
  };
}

function pageFixture(overrides: Partial<CashflowData> = {}): CashflowData {
  const september = monthFixture();
  return {
    currency: "SGD",
    hasTransactions: true,
    oldestYear: 2024,
    light: {
      colour: "yellow",
      reasons: [{ code: "kept_under_10", keptBasisPoints: 600, cushionTenths: null, billName: "" }],
    },
    average: { months: 3, inMinor: 500000, outMinor: 450000, leftMinor: 50000, keptBasisPoints: 1000 },
    cushion: { known: true, tenths: 42, reason: "" },
    lastFinished: september,
    current: monthFixture({ month: "2026-10", status: "current", inMinor: 120000, outMinor: 80000, leftMinor: 40000 }),
    recent: [monthFixture({ month: "2026-08" }), september],
    year: {
      year: 2026,
      months: [september],
      keptSoFarMinor: 900000,
      incomeMinor: 4500000,
      greyMonths: 0,
      keptBasisPoints: 2000,
      sameMonthsLastYearMinor: 700000,
      headingForMinor: 1100000,
      billsToCome: [
        { billId: "bill-1", name: "Car insurance", amountMinor: 210000, nextDue: "2026-12-01", unconvertible: false },
      ],
    },
    ...overrides,
  };
}

function renderPage(data: CashflowData, extra: Record<string, { status: number; body: unknown }> = {}) {
  const fetchMock = stubFetchRoutes({
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/cashflow": { status: 200, body: data },
    ...extra,
  });
  renderWithRouter(<CashflowPage />);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("CashflowPage light", () => {
  it("shows the light with its reason in words", async () => {
    renderPage(pageFixture());
    const light = await screen.findByTestId("cashflow-light");
    expect(within(light).getByText("Watch")).toBeInTheDocument();
    expect(
      within(light).getByText("You kept 6% of income on average over the last 3 months"),
    ).toBeInTheDocument();
  });

  it("reads 'On track by common guidelines' for green", async () => {
    renderPage(pageFixture({ light: { colour: "green", reasons: [] } }));
    expect(await screen.findByText("On track by common guidelines")).toBeInTheDocument();
  });
});

describe("cashflowCopy", () => {
  it("never uses the word safe", () => {
    const texts: string[] = [];
    for (const code of cashflowReasonCodeSchema.options) {
      for (const tenths of [null, 0, 9, 10, 29, 30]) {
        for (const kept of [null, 0, 600, 1000]) {
          const reason: CashflowReason = { code, keptBasisPoints: kept, cushionTenths: tenths, billName: "Rent" };
          texts.push(reasonText(reason, 3), reasonText(reason, 1));
        }
      }
    }
    for (const grey of ["no_income", "under_half_usual", "unconvertible"] as const) {
      texts.push(greyReasonText(grey, ["IDR"]));
    }
    for (const reason of ["", "no_cash_accounts", "unconvertible_account", "no_solid_months", "no_spending"] as const) {
      texts.push(cushionText({ known: reason === "", tenths: reason === "" ? 42 : null, reason }));
    }
    texts.push(...Object.values(cashflowCopy).flatMap((value) => (typeof value === "string" ? [value] : [])));
    texts.push(...cashflowCopy.infoPanel);
    for (const text of texts) {
      expect(text).not.toMatch(/\bsafe\b/i);
    }
  });

  it("says 'just under' when a figure equals its threshold after rounding", () => {
    const base = { keptBasisPoints: null, cushionTenths: null, billName: "" };
    expect(reasonText({ ...base, code: "cushion_under_3", cushionTenths: 30 }, 3)).toContain("just under 3 months");
    expect(reasonText({ ...base, code: "cushion_under_1", cushionTenths: 10 }, 3)).toContain("just under 1 month");
    expect(reasonText({ ...base, code: "kept_under_10", keptBasisPoints: 1000 }, 3)).toContain("just under 10%");
    expect(reasonText({ ...base, code: "cushion_under_3", cushionTenths: 24 }, 3)).toContain("2.4 months");
  });
});

describe("CashflowMonthly", () => {
  it("the current month has no colour", async () => {
    renderPage(pageFixture());
    const current = await screen.findByTestId("cashflow-current");
    expect(current).toHaveAttribute("data-tone", "none");
    expect(current).toHaveTextContent("S$400.00");
    expect(current).toHaveTextContent(/so far/i);
  });

  it("money out names the spread bills and loan repayments", async () => {
    const last = monthFixture({ spreadMinor: 31000, loanMinor: 180000 });
    renderPage(pageFixture({ lastFinished: last }));
    const out = await screen.findByTestId("cashflow-figure-out");
    expect(out).toHaveTextContent("includes S$310.00 of yearly bills spread");
    expect(out).toHaveTextContent("includes S$1,800.00 of loan repayments");
  });

  it("leaves both notes off money out when there is nothing to name", async () => {
    renderPage(pageFixture());
    const out = await screen.findByTestId("cashflow-figure-out");
    expect(out).not.toHaveTextContent("includes");
  });

  it("the average says how many months it covers", async () => {
    renderPage(pageFixture({ average: { months: 2, inMinor: 1, outMinor: 1, leftMinor: 0, keptBasisPoints: null } }));
    expect(await screen.findByText("Average of 2 months")).toBeInTheDocument();
  });

  it("calls a full window the 3-month average", async () => {
    renderPage(pageFixture());
    expect(await screen.findByText("3-month average")).toBeInTheDocument();
  });
});

describe("CashflowMonthTable", () => {
  const grey = monthFixture({ month: "2026-08", status: "grey", grey: ["no_income"], inMinor: 0, keptBasisPoints: null });

  it("a grey month offers count it and confirming calls the API", async () => {
    const fetchMock = renderPage(pageFixture({ recent: [grey] }), {
      "PUT /api/v1/cashflow/confirmations/2026-08": {
        status: 200,
        body: { month: "2026-08", confirmedAt: "2026-10-25T08:00:00Z", confirmedByName: "Alex" },
      },
    });
    const button = await screen.findByRole("button", { name: "Looks right, count it" });
    expect(screen.getByText("No income recorded")).toBeInTheDocument();
    fireEvent.click(button);
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([url, init]) => {
        return String(url) === "/api/v1/cashflow/confirmations/2026-08" && init?.method === "PUT";
      })).toBe(true);
    });
  });

  it("a confirmed month shows who confirmed it and can be undone", async () => {
    const confirmed = monthFixture({
      month: "2026-08",
      status: "solid",
      confirmation: { confirmedAt: "2026-10-25T23:30:00Z", confirmedByName: "Alex" },
    });
    const fetchMock = renderPage(pageFixture({ recent: [confirmed] }), {
      "DELETE /api/v1/cashflow/confirmations/2026-08": { status: 204, body: undefined },
    });
    expect(await screen.findByText("Counted by Alex on 25 Oct 2026")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Looks right, count it" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Undo" }));
    await waitFor(() => {
      expect(fetchMock.mock.calls.some(([url, init]) => {
        return String(url) === "/api/v1/cashflow/confirmations/2026-08" && init?.method === "DELETE";
      })).toBe(true);
    });
  });

  it("an unconvertible-only grey month does not offer count it", async () => {
    const unconvertible = monthFixture({
      month: "2026-08",
      status: "grey",
      grey: ["unconvertible"],
      unconvertibleCurrencies: ["IDR"],
    });
    renderPage(pageFixture({ recent: [unconvertible] }));
    expect(await screen.findByText("An IDR transaction couldn't be converted")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Looks right, count it" })).not.toBeInTheDocument();
  });

  it("does not offer count it when a month is also unconvertible", async () => {
    const mixed = monthFixture({ month: "2026-08", status: "grey", grey: ["no_income", "unconvertible"], unconvertibleCurrencies: ["IDR"] });
    renderPage(pageFixture({ recent: [mixed] }));
    await screen.findByText("An IDR transaction couldn't be converted");
    expect(screen.queryByRole("button", { name: "Looks right, count it" })).not.toBeInTheDocument();
  });

  it("month detail links to the ledger at that month", async () => {
    const withBills = monthFixture({
      month: "2026-09",
      spreadMinor: 17500,
      contributions: [{ kind: "spread_bill", name: "Car insurance", amountMinor: 17500, paidOn: "2026-03-14" }],
    });
    renderPage(pageFixture({ recent: [withBills], lastFinished: withBills }));
    fireEvent.click(await screen.findByRole("button", { name: /September 2026/ }));
    const detail = await screen.findByTestId("cashflow-month-detail");
    expect(within(detail).getByText("Car insurance")).toBeInTheDocument();
    expect(within(detail).getByText("S$175.00")).toBeInTheDocument();
    const link = within(detail).getByRole("link", { name: /transactions/i });
    expect(link.getAttribute("href")).toContain("/money/transactions");
    expect(link.getAttribute("href")).toContain("month=2026-09");
  });
});

describe("CashflowYearly", () => {
  function openYearly() {
    fireEvent.click(screen.getByRole("tab", { name: "Yearly" }));
  }

  it("the yearly tab shows heading for only in the current year", async () => {
    renderPage(pageFixture());
    await screen.findByRole("tab", { name: "Yearly" });
    openYearly();
    expect(await screen.findByText("Heading for")).toBeInTheDocument();
    expect(screen.getByText("S$11,000.00")).toBeInTheDocument();
  });

  it("leaves heading for out of a past year", async () => {
    const data = pageFixture();
    renderPage({ ...data, year: { ...data.year, year: 2025, headingForMinor: null } });
    await screen.findByRole("tab", { name: "Yearly" });
    openYearly();
    await screen.findByText("Kept so far");
    expect(screen.queryByText("Heading for")).not.toBeInTheDocument();
  });

  it("says how many months lack enough data and lists bills still to come", async () => {
    const data = pageFixture();
    renderPage({ ...data, year: { ...data.year, greyMonths: 2 } });
    await screen.findByRole("tab", { name: "Yearly" });
    openYearly();
    expect(await screen.findByText("includes 2 months with not enough data")).toBeInTheDocument();
    expect(screen.getByText("Car insurance")).toBeInTheDocument();
    expect(screen.getByText("S$2,100.00")).toBeInTheDocument();
  });

  it("steps the year picker only within the years that have data", async () => {
    const fetchMock = renderPage(pageFixture(), {
      "GET /api/v1/cashflow?year=2025": { status: 200, body: pageFixture({ year: { ...pageFixture().year, year: 2025, headingForMinor: null } }) },
    });
    await screen.findByRole("tab", { name: "Yearly" });
    openYearly();
    expect(await screen.findByRole("button", { name: "Next year" })).toBeDisabled();
    fireEvent.click(screen.getByRole("button", { name: "Previous year" }));
    await waitFor(() => expect(screen.getByTestId("cashflow-year")).toHaveTextContent("2025"));
    expect(fetchMock.mock.calls.some(([url]) => String(url) === "/api/v1/cashflow?year=2025")).toBe(true);
  });
});

describe("CashflowChart", () => {
  it("draws losses below the zero line and styles each kind of month", async () => {
    const months = [
      monthFixture({ month: "2026-06", leftMinor: 100000 }),
      monthFixture({ month: "2026-07", leftMinor: -50000 }),
      monthFixture({ month: "2026-08", status: "grey", grey: ["no_income"] }),
      monthFixture({ month: "2026-09", status: "current", leftMinor: 20000 }),
      monthFixture({ month: "2026-10", status: "future", leftMinor: 0, projectedLeftMinor: 60000 }),
    ];
    renderPage(pageFixture({ recent: months }));
    const chart = await screen.findByTestId("cashflow-chart");
    const baseline = Number(chart.getAttribute("data-baseline"));
    const bars = within(chart).getAllByTestId("cashflow-bar");
    const by = (m: string) => bars.find((bar) => bar.getAttribute("data-month") === m)!;
    expect(by("2026-06").getAttribute("data-style")).toBe("solid-positive");
    expect(by("2026-07").getAttribute("data-style")).toBe("solid-negative");
    expect(Number(by("2026-07").getAttribute("y"))).toBeGreaterThanOrEqual(baseline);
    expect(Number(by("2026-06").getAttribute("y")) + Number(by("2026-06").getAttribute("height"))).toBeLessThanOrEqual(baseline + 0.001);
    expect(by("2026-08").getAttribute("data-style")).toBe("grey");
    expect(by("2026-09").getAttribute("data-style")).toBe("current");
    expect(by("2026-10").getAttribute("data-style")).toBe("projected");
  });
});

describe("CashflowPage states", () => {
  it("the empty state links to add a transaction", async () => {
    renderPage(pageFixture({ hasTransactions: false, oldestYear: 0, lastFinished: null }));
    expect(await screen.findByText(/log income and spending for a month to see cashflow/i)).toBeInTheDocument();
    const link = screen.getByRole("link", { name: "Add a transaction" });
    expect(link).toHaveAttribute("href", "/money/transactions");
  });

  it("explains the thresholds in the info panel", async () => {
    renderPage(pageFixture());
    fireEvent.click(await screen.findByRole("button", { name: "About these numbers" }));
    const panel = screen.getByTestId("cashflow-info");
    expect(panel).toHaveTextContent(/MoneySense/);
    expect(panel).toHaveTextContent(/not advice/i);
    expect(panel).toHaveTextContent(/enter it as a bill/i);
    expect(panel).toHaveTextContent(/transfer/i);
  });

  it("shows the owner-only message on a 403", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/cashflow": { status: 403, body: { error: "forbidden" } },
    });
    renderWithRouter(<CashflowPage />);
    expect(await screen.findByTestId("cashflow-owner-only")).toBeInTheDocument();
  });

  it("shows a load error otherwise", async () => {
    stubFetchRoutes({
      "GET /api/v1/currencies": CURRENCIES,
      "GET /api/v1/cashflow": { status: 500, body: { error: "boom" } },
    });
    renderWithRouter(<CashflowPage />);
    expect(await screen.findByTestId("cashflow-load-error")).toBeInTheDocument();
  });
});
