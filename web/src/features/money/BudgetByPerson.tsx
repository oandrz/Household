// The "Spending by person" card: one row per member with an expense this
// month, each bar sized relative to the group's own total spend -- the same
// proportional read the design's mockup numbers show (its three bars' widths
// sum to the household's total, not to some fixed budget figure). No
// synthetic "Kids (shared)" grouping (spec decision, formulas table): members
// render individually, whatever the server's byPerson list contains.
import { formatMoney } from "./formatMoney";
import type { BudgetMonthResponse } from "./budgetSchemas";

type PersonLine = BudgetMonthResponse["byPerson"][number];

export function BudgetByPerson({
  people,
  currency,
  symbol,
}: {
  people: PersonLine[];
  currency: string;
  symbol?: string;
}) {
  const totalMinor = people.reduce((sum, person) => sum + person.spentMinor, 0);

  return (
    <div className="flex flex-col gap-3.5">
      {people.map((person) => {
        // Guards totalMinor === 0 the same way BudgetCategoryGrid's
        // barWidthPercent does -- a month with people listed but nothing
        // spent yet must render flat bars, not `NaN%`.
        const width = totalMinor > 0 ? (person.spentMinor / totalMinor) * 100 : 0;
        return (
          <div key={person.membershipId} data-testid="budget-person-row" className="flex items-center gap-3">
            <div className="flex h-[30px] w-[30px] flex-none items-center justify-center rounded-full bg-callout text-[12px] font-semibold text-accent">
              {person.name.slice(0, 1).toUpperCase()}
            </div>
            <div className="flex-1">
              <div className="flex justify-between text-[13px]">
                <span className="text-ink">{person.name}</span>
                <span className="font-semibold text-ink">
                  {formatMoney(person.spentMinor, currency, symbol)}
                </span>
              </div>
              {/* bg-canvas, not bg-hairline -- see BudgetCategoryGrid.tsx's
                  own comment on why a border tint is too faint at 5px tall. */}
              <div className="mt-1.5 h-[5px] overflow-hidden rounded-full bg-canvas">
                <div className="h-full bg-accent" style={{ width: `${width}%` }} />
              </div>
            </div>
          </div>
        );
      })}
    </div>
  );
}
