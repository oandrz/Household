// Copy for the Finances screen, kept in a plain .ts module for the same
// reason features/auth/copy.ts and features/settings/copy.ts are -- so
// eslint's react-refresh/only-export-components rule never has to think
// about a file that mixes components with other exports.
export const FINANCES_COPY = {
  title: "Finances",
  netWorth: "Net worth",
  assetsAndLiabilities: "Assets & liabilities",
  net: "Net",
  accounts: "Accounts",
  addAccount: "+ Add account",
  // First run. Every household sees this immediately after signing up, so it
  // is a real screen rather than an edge case.
  emptyTitle: "Nothing here yet.",
  emptyBody: "Add what your household owns and owes, and Hearth will keep the total.",
  // No account could be converted into the household's currency. Never a zero
  // -- zero is a claim about their money, and the truth is that we cannot
  // compute it.
  notComputable: (household: string, others: string) =>
    `We can't work out a total yet: there's no exchange rate between ${household} and ${others}.`,
  excludedNoRate: (count: number, currencies: string) =>
    `${count} ${count === 1 ? "account" : "accounts"} not included: no exchange rate for ${currencies}.`,
  excludedByChoice: (count: number) =>
    `${count} ${count === 1 ? "account is" : "accounts are"} set not to count toward net worth.`,
  // The bars will not always sum to net worth, because an account can be in
  // the breakdown and out of the total. Say so rather than letting it look
  // like an arithmetic bug.
  breakdownFootnote: "Includes accounts that don't count toward net worth.",
  limitedEmpty: "No accounts have been shared with you yet.",
  archivedToggle: "Show archived",
  archivedEmpty: "No archived accounts.",
  // The recent-transactions strip (Task 17). Deferred by the accounts spec
  // for having no data -- Transactions now supplies it.
  recentTransactions: "Recent transactions",
  seeAll: "See all",
  // The twelve-month trend (design line 354).
  trendWindow: "Last 12 months",
  // Fewer than two known months. Every household is in this state on their
  // first day, so it is a real screen and not an edge case.
  trendEmpty: "Not enough history yet — the chart starts once there are two months to compare.",
  trendIncomplete: (from: string) =>
    `Lighter bars, before ${from}, are missing accounts that were added later.`,
  // The same note when no month in the window is complete, so there is no
  // "before" to name.
  trendIncompleteUnknownStart: "Lighter bars are missing accounts that were added later.",
  // 210 -> "▲ 2.1%". The arrow carries the direction as well as the sign, so
  // the figure reads at a glance and not only to someone checking for a minus.
  trendChange: (basisPoints: number) =>
    `${basisPoints < 0 ? "▼" : "▲"} ${(Math.abs(basisPoints) / 100).toFixed(1)}%`,
  trendChartLabel: (from: string, to: string, known: number) =>
    `Net worth from ${from} to ${to}. ${known} of 12 months have a figure.`,
} as const;

// "2025-08" -> "Aug '25", the design's own axis format. Deliberately not
// shared with retroCopy's monthShortLabel, which is a different format (no
// year) for a different chart -- one helper reused two ways would have to
// grow a mode parameter to serve both.
export function monthTickLabel(month: string): string {
  const [year, monthNumber] = month.split("-").map(Number);
  const short = new Date(year, monthNumber - 1, 2).toLocaleDateString("en-US", { month: "short" });
  return `${short} '${String(year).slice(-2)}`;
}
