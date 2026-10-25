// Every sentence on the Cashflow page. The server sends reason CODES; this
// file is the only place they become words, so the wording rule lives in one
// spot: state facts, never advice, and never call a household "safe". Green
// reads "On track by common guidelines".
import type {
  CashflowColour,
  CashflowGreyReason,
  CashflowMonth,
  CashflowPage,
  CashflowReason,
} from "./cashflowSchemas";

type CashflowCushion = CashflowPage["cushion"];
type CashflowConfirmation = NonNullable<CashflowMonth["confirmation"]>;

export const LIGHT_LABEL: Record<CashflowColour, string> = {
  green: "On track by common guidelines",
  yellow: "Watch",
  red: "At risk",
  grey: "Not enough data",
};

const MONTH_ABBREVIATIONS = [
  "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
];

// "2026-10-25" or an RFC 3339 timestamp -> "25 Oct 2026". Only the date part
// is read and nothing is converted between time zones, so a confirmation
// shows the day it was recorded under, wherever the viewer is.
export function dateLabel(isoDate: string): string {
  const [year, month, day] = isoDate.slice(0, 10).split("-").map(Number);
  return `${day} ${MONTH_ABBREVIATIONS[month - 1]} ${year}`;
}

// "2026-09" -> "Sep".
export function shortMonthLabel(month: string): string {
  return MONTH_ABBREVIATIONS[Number(month.slice(5, 7)) - 1];
}

// Basis points as a percentage without a float: 600 -> "6", 1250 -> "12.5".
export function percentText(basisPoints: number): string {
  const sign = basisPoints < 0 ? "-" : "";
  const magnitude = Math.abs(basisPoints);
  const whole = Math.floor(magnitude / 100);
  const hundredths = magnitude % 100;
  if (hundredths === 0) return `${sign}${whole}`;
  const fraction = String(hundredths).padStart(2, "0").replace(/0$/, "");
  return `${sign}${whole}.${fraction}`;
}

// "19% of income kept", or "Kept: —" when income was zero and a share is
// undefined.
export function keptLine(basisPoints: number | null): string {
  return basisPoints === null ? "Kept: —" : `${percentText(basisPoints)}% of income kept`;
}

export function keptCell(basisPoints: number | null): string {
  return basisPoints === null ? "—" : `${percentText(basisPoints)}%`;
}

// Tenths of a month as text: 42 -> "4.2".
function tenthsText(tenths: number): string {
  const sign = tenths < 0 ? "-" : "";
  const magnitude = Math.abs(tenths);
  return `${sign}${Math.floor(magnitude / 10)}.${magnitude % 10}`;
}

function windowText(averageMonths: number): string {
  if (averageMonths <= 1) return "the last month";
  return `the last ${averageMonths} months`;
}

// A figure that rounds to its own threshold ("3.0 months" under a 3 month
// line) would read as a contradiction, so it is worded "just under" instead.
function cushionPhrase(tenths: number, thresholdMonths: 1 | 3): string {
  if (tenths >= thresholdMonths * 10) {
    return `just under ${thresholdMonths} ${thresholdMonths === 1 ? "month" : "months"}`;
  }
  return `${tenthsText(tenths)} months, under the ${thresholdMonths}-month guideline`;
}

// averageMonths is how many months the page's average really covers, so the
// sentence never claims a wider window than the figure used.
export function reasonText(reason: CashflowReason, averageMonths: number): string {
  switch (reason.code) {
    case "no_finished_month":
      return "There is no finished month to judge yet";
    case "last_month_not_enough_data":
      return "Last month does not have enough data to count yet";
    case "no_months_to_average":
      return "None of the last 3 finished months has enough data to average";
    case "left_not_positive":
      return `Nothing was left over after spending, on average over ${windowText(averageMonths)}`;
    case "kept_under_10": {
      const basisPoints = reason.keptBasisPoints;
      const figure = basisPoints === null
          ? "under 10%"
          : basisPoints >= 1000
            ? "just under 10%"
            : `${percentText(basisPoints)}%`;
      return `You kept ${figure} of income on average over ${windowText(averageMonths)}`;
    }
    case "last_month_negative":
      return "Last month, more went out than came in";
    case "cushion_under_1":
      return `Your cash accounts cover ${reason.cushionTenths === null ? "under 1 month" : cushionPhrase(reason.cushionTenths, 1)} of spending`;
    case "cushion_under_3":
      return `Your cash accounts cover ${reason.cushionTenths === null ? "under 3 months" : cushionPhrase(reason.cushionTenths, 3)} of spending`;
    case "cushion_unknown":
      return "The cash cushion could not be worked out";
    case "bill_due_soon":
      // With no exchange rate nothing was compared, so the sentence must not
      // claim the bill is larger.
      return reason.billUnconvertible
        ? `${reason.billName} is due within 60 days and its amount couldn't be converted, so it can't be compared with the average left over`
        : `${reason.billName} is due within 60 days and is larger than the average left over each month`;
    case "short_average":
      // Worded to be true for a new household and for a long history with a
      // grey month in the window alike.
      return `Only ${averageMonths} of the last 3 months can be counted. Hearth needs all 3 before it can say on track. A month marked not enough data can sometimes be counted with “Looks right, count it”.`;
    case "spending_not_checked":
      return "Hearth can't yet tell whether some spending is missing: it needs 3 earlier finished months to compare each month against.";
    default:
      // A code this build has never heard of: say so rather than guess.
      return "This check could not be described";
  }
}

export function greyReasonText(reason: CashflowGreyReason, unconvertibleCurrencies: string[]): string {
  switch (reason) {
    case "no_income":
      return "No income recorded";
    case "under_half_usual":
      return "Spending is under half of usual";
    case "unconvertible": {
      return unconvertibleCurrencies.length > 0
        ? `An ${unconvertibleCurrencies.join(", ")} transaction couldn't be converted`
        : "A foreign-currency transaction couldn't be converted";
    }
    default:
      return "Not enough data";
  }
}

export function cushionText(cushion: CashflowCushion): string {
  if (cushion.known && cushion.tenths !== null) return `${tenthsText(cushion.tenths)} months`;
  return "—";
}

export function cushionReasonText(cushion: CashflowCushion): string {
  switch (cushion.reason) {
    case "":
      return "Cash in bank and cash accounts, against average monthly spending";
    case "no_cash_accounts":
      return "No bank or cash accounts recorded";
    case "unconvertible_account":
      return "An account could not be converted to your main currency";
    case "no_solid_months":
      return "No finished month has enough data to compare against";
    case "no_spending":
      return "No spending recorded to compare against";
    default:
      return "Could not be worked out";
  }
}

export function confirmationText(confirmation: CashflowConfirmation): string {
  return `Counted by ${confirmation.confirmedByName} on ${dateLabel(confirmation.confirmedAt)}`;
}

export const cashflowCopy = {
  title: "Cashflow",
  subtitle: "What came in, what went out, and what was left over",
  monthlyTab: "Monthly",
  yearlyTab: "Yearly",
  infoButton: "About these numbers",
  loading: "Loading…",
  loadError: "Cashflow could not be loaded. Try refreshing the page.",
  ownerOnlyHeading: "Owner only",
  ownerOnlyBody: "Cashflow is visible to the household owner. Ask them if you'd like to see where things stand.",
  emptyHeadline: "Nothing to show yet",
  emptyBody: "Log income and spending for a month to see cashflow.",
  addTransaction: "Add a transaction",

  moneyIn: "Money in",
  moneyOut: "Money out",
  leftOver: "Left over",
  spreadNote: (amount: string) => `includes ${amount} of yearly bills spread`,
  loanNote: (amount: string) => `includes ${amount} of loan repayments`,
  noFinishedMonth: "No month has finished yet.",
  average: "3-month average",
  averageOf: (months: number) => `Average of ${months} ${months === 1 ? "month" : "months"}`,
  noAverage: "No finished month has enough data to average yet.",
  soFar: (monthName: string) => `${monthName} so far`,
  soFarNote: "Not finished, so it is not counted in the light or the average.",

  tableMonth: "Month",
  tableKept: "Kept",
  tableLight: "Light",
  countIt: "Looks right, count it",
  undo: "Undo",
  confirmError: "That could not be saved. Try again.",
  rowLeftOver: "Money left over",
  rowNothingLeft: "Nothing left over",
  rowGrey: "Not enough data",
  rowCurrent: "So far",
  detailsNone: "No yearly bills spread and no loan repayments in this month.",
  detailsSpread: "Yearly or quarterly bill, spread",
  detailsLoan: "Loan repayment",
  detailsPaidOn: (date: string) => `paid ${date}`,
  detailsLedger: "See the transactions behind this month",
  detailsClose: "Close",
  transactionCount: (count: number) => `${count} ${count === 1 ? "transaction" : "transactions"}`,

  keptSoFar: "Kept so far",
  sameMonthsLastYear: (amount: string) => `${amount} in the same months last year`,
  noLastYear: "No data for the same months last year",
  greyMonths: (count: number) => `includes ${count} ${count === 1 ? "month" : "months"} with not enough data`,
  headingFor: "Heading for",
  headingForNote: (averageMonths: number) =>
    `Kept so far plus the ${averageMonths < 3 ? `average of ${averageMonths} ${averageMonths === 1 ? "month" : "months"}` : "3-month average"} for each month still to come`,
  shareKept: (percent: string) => `${percent}% of income kept this year`,
  cashCushion: "Cash cushion",
  billsToCome: "Bills still to come this year",
  billsToComeNote: "For information. Once paid, each is spread across the months it covers.",
  billsNone: "No quarterly or yearly bills are due before the end of the year.",
  billUnconvertible: "can't be converted",
  previousYear: "Previous year",
  nextYear: "Next year",

  chartLabel: "Left over each month",
  chartLegend: {
    solid: "Finished",
    grey: "Not enough data",
    current: "This month so far",
    projected: "Projected",
  },

  infoTitle: "How to read this page",
  infoPanel: [
    "The checks use general guidelines from MoneySense: keep at least 10% of income, and 3 to 6 months of spending in cash. They are guidelines, not advice.",
    "The light never goes above Watch until it can count all of the last 3 finished months, and each of them has been compared with at least 3 counted months before it or marked “Looks right, count it”. Until then, a month with spending missing cannot be told apart from a good one.",
    "Yearly and quarterly bills are spread over the months they cover, so one big premium does not turn a single month red.",
    "A yearly cost entered as a plain expense is not spread. Enter it as a bill instead.",
    "A loan set up as a bill should not also be logged as a transfer, or it counts twice.",
  ],
} as const;
