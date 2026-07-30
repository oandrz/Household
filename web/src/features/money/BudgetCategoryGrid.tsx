// The Categories card: one row per budget line with a spent/cap bar, the
// design's "over" styling, and an archived marker for a line whose category
// has since been archived (budgetSchemas.ts's own comment on why archived
// categories still render here -- history stays true even after the
// category stops being offered for new caps).
import { BUDGET_COPY } from "./budgetCopy";
import { formatMoney } from "./formatMoney";
import type { BudgetMonthResponse } from "./budgetSchemas";

type CategoryLine = BudgetMonthResponse["categories"][number];

// The bar's fill width. Guards capMinor === 0 separately from the general
// division -- a category with no cap line is possible (buildCategoryViews's
// own comment: it renders at zero, never "over," unless it actually has a
// cap) but can still show real spend once transactions exist, and 0/0 must
// never reach the DOM as `NaN%`.
function barWidthPercent(spentMinor: number, capMinor: number): number {
  if (capMinor <= 0) return spentMinor > 0 ? 100 : 0;
  return Math.min(100, (spentMinor / capMinor) * 100);
}

function CategoryRow({
  category,
  currency,
  symbol,
}: {
  category: CategoryLine;
  currency: string;
  symbol?: string;
}) {
  const width = barWidthPercent(category.spentMinor, category.capMinor);
  return (
    <div data-testid="budget-category-row" className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2 text-[13px]">
        <span className={category.archived ? "text-muted" : "text-ink"}>
          {category.name}
          {category.archived && (
            <span data-testid="budget-category-archived" className="ml-1.5 text-[11px] font-normal text-muted">
              {BUDGET_COPY.archivedMarker}
            </span>
          )}
        </span>
        <span className={category.over ? "font-semibold text-danger" : "text-muted"}>
          {formatMoney(category.spentMinor, currency, symbol)} /{" "}
          {formatMoney(category.capMinor, currency, symbol)}
          {category.over && ` ${BUDGET_COPY.overMarker}`}
        </span>
      </div>
      {/* bg-canvas, not bg-hairline: hairline is a near-transparent border
          tint (rgba(0,0,0,.08)) meant for 1px lines, and at 5px tall over a
          white card it reads as barely-there. canvas is the app's own
          opaque neutral background colour, close to the design's #f1f0ec
          track. */}
      <div className="h-[5px] overflow-hidden rounded-full bg-canvas">
        <div
          className={`h-full ${category.over ? "bg-danger" : "bg-accent"}`}
          style={{ width: `${width}%` }}
        />
      </div>
    </div>
  );
}

export function BudgetCategoryGrid({
  categories,
  currency,
  symbol,
}: {
  categories: CategoryLine[];
  currency: string;
  symbol?: string;
}) {
  return (
    <div className="grid grid-cols-1 gap-x-7 gap-y-4 sm:grid-cols-2">
      {categories.map((category) => (
        <CategoryRow key={category.categoryId} category={category} currency={currency} symbol={symbol} />
      ))}
    </div>
  );
}
