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
} as const;
