// Formatting for a "YYYY-MM" month string. Which month it is NOW is not
// decided here: that is the household's month, lib/householdDate.ts's
// monthIn(zone).

// "2026-07" -> "July 2026", for the month headings on TransactionsPage and
// BudgetPage (both receive the month as "YYYY-MM"). Parsed onto day 2 of the
// month, not day 1 -- day 1 at a negative UTC offset can read back as the
// *previous* month once `new Date(year, month, day)` applies the runtime's
// local timezone, and day 2 has no such edge for any real-world offset.
export function monthLabel(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", {
    month: "long",
    year: "numeric",
  });
}
