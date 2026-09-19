// Local calendar date, never toISOString() -- a UTC conversion can read back
// yesterday's (or tomorrow's) month for a household west or east of UTC, which
// is the same mistake the backend's dateOnly hit and AccountModal's today()
// guards against. Shared by BudgetPage and Overview, which both ask
// GET /budgets/{month} about "this month" and must agree on which month that is.
export function currentMonth(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
}

// "2026-07" -> "July 2026", for the month headings on TransactionsPage and
// BudgetPage (both receive the month as "YYYY-MM"). Parsed onto day 2 of the
// month, not day 1 -- day 1 at a negative UTC offset can read back as the
// *previous* month once `new Date(year, month, day)` applies the runtime's
// local timezone, and day 2 has no such edge for any real-world offset. The
// same reasoning as AccountModal's own today().
export function monthLabel(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", {
    month: "long",
    year: "numeric",
  });
}
