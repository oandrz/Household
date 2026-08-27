// The "Spending by person" card: one row per member with an expense this
// month, each bar sized relative to the group's own total spend -- the same
// proportional read the design's mockup numbers show (its three bars' widths
// sum to the household's total, not to some fixed budget figure). No
// synthetic "Kids (shared)" grouping (spec decision, formulas table): members
// render individually, whatever the server's byPerson list contains.
//
// The server's `byPerson` list can carry one more row than "one per member":
// `membershipId: ""` for spend nobody paid -- a hand-entered transaction
// saved without a payer, or (once Bills ships) a bill with no "Paid by".
// That row is not the "Kids (shared)" grouping the sentence above rejects --
// it attributes spend to nobody, it only names the absence of a payer, which
// is what lets these rows sum to the Spent figure above the card (see
// budget.go's BudgetPersonView doc comment). Its label and explanation are
// supplied here, never by the server: `name` arrives "" on the wire on
// purpose (budgetCopy.ts's own comment on `unattributed`).
import { formatMoney } from "./formatMoney";
import { BUDGET_COPY } from "./budgetCopy";
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
  const hasUnattributed = people.some((person) => person.membershipId === "");

  return (
    <div className="flex flex-col gap-3.5">
      {people.map((person) => {
        // Guards totalMinor === 0 the same way BudgetCategoryGrid's
        // barWidthPercent does -- a month with people listed but nothing
        // spent yet must render flat bars, not `NaN%`.
        const width = totalMinor > 0 ? (person.spentMinor / totalMinor) * 100 : 0;
        // The unattributed row's name arrives "" on the wire -- label is
        // derived once here so both the avatar initial and the name span
        // use it, rather than the avatar rendering blank next to a labelled
        // row (the "did I trigger a bug" read this card exists to avoid).
        const label = person.membershipId === "" ? BUDGET_COPY.unattributed : person.name;
        return (
          <div key={person.membershipId} data-testid="budget-person-row" className="flex items-center gap-3">
            <div className="flex h-[30px] w-[30px] flex-none items-center justify-center rounded-full bg-callout text-[12px] font-semibold text-accent">
              {label.slice(0, 1).toUpperCase()}
            </div>
            <div className="flex-1">
              <div className="flex justify-between text-[13px]">
                <span className="text-ink">{label}</span>
                <span className="tabular font-semibold text-ink">
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
      {/* Gated on the row actually being present: a household with no
          unattributed spend this month must not see an explanation for a
          row that isn't there. */}
      {hasUnattributed && (
        <p data-testid="budget-unattributed-explanation" className="text-[12px] leading-relaxed text-muted">
          {BUDGET_COPY.unattributedExplanation}
        </p>
      )}
    </div>
  );
}
