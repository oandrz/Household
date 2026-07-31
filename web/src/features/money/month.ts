// Local calendar date, never toISOString() -- a UTC conversion can read back
// yesterday's (or tomorrow's) month for a household west or east of UTC, which
// is the same mistake the backend's dateOnly hit and AccountModal's today()
// guards against. Shared by BudgetPage and Overview, which both ask
// GET /budgets/{month} about "this month" and must agree on which month that is.
export function currentMonth(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
}
