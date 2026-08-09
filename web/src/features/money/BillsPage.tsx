// The Bills screen: a shell only, per this task's own scope boundary. It
// exists to prove /money/bills mounts and useBills.ts is wired to it --
// stat cards, the three lists (Due soon / Later / Paid this month), all
// five states and archive/restore are Task 12's, not built or tested here.
// Composition only, same as GoalsPage.tsx/BudgetPage.tsx: no `apiFetch`
// call belongs in this file, ever -- fetch orchestration lives in
// useBills.ts.
import { useBills } from "./useBills";

export function BillsPage() {
  const bills = useBills(false);

  if (bills.isLoading) {
    return <p className="p-9 text-xs text-muted">Loading…</p>;
  }

  if (bills.error) {
    return (
      <p role="alert" data-testid="bills-load-error" className="p-9 text-xs text-danger">
        Bills could not be loaded.
      </p>
    );
  }

  return (
    <div data-testid="bills-page" className="flex flex-col gap-5 px-9 py-8">
      <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">Bills</h1>
    </div>
  );
}
