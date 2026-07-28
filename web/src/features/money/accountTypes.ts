import type { AccountType } from "./schemas";

// The five types, in the order the breakdown chart draws them: assets first,
// then debts. Mirrors domain.AccountTypes() -- the two lists must not drift,
// and this comment is where someone adding a sixth will look.
export const ACCOUNT_TYPES: AccountType[] = [
  "cash",
  "investment",
  "property",
  "loan",
  "credit_card",
];

export const ACCOUNT_TYPE_LABELS: Record<AccountType, string> = {
  cash: "Cash & savings",
  investment: "Investments",
  property: "Property",
  loan: "Loan",
  credit_card: "Credit card",
};

export const LIABILITY_TYPES: ReadonlySet<AccountType> = new Set(["loan", "credit_card"]);
