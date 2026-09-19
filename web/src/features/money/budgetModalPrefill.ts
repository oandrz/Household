// Which prefill the Budget modal opens with, for each of the five ways into it
// on BudgetPage. Kept pure and out of the page so the decision is tested
// without rendering the screen; BudgetPage only wires a click to an entry.
import type { BudgetMonthResponse } from "./budgetSchemas";
import {
  familyOfFourTemplate,
  fiftyThirtyTwentyTemplate,
  type TemplatePrefill,
} from "./budgetTemplates";
import type { Category } from "./transactionSchemas";

// The modal handoff BudgetModal consumes: `prefill: null` is a blank budget
// ("Create your first budget"), a `TemplatePrefill` is a template's computed
// starting point. `awaitingIncome` is set only by the 50/30/20 card -- its
// prefill has zero lines until an income figure exists, and the modal uses
// this flag, not "lines.length === 0" alone, to decide whether to show the
// income prompt (a household with genuinely no matching categories would also
// have zero lines, for an unrelated reason).
export type BudgetModalState = {
  prefill: TemplatePrefill | null;
  awaitingIncome: boolean;
};

export type BudgetModalEntry =
  | "blank"
  | "familyOfFour"
  | "fiftyThirtyTwenty"
  | "editBudget"
  | "importLastMonth";

type SavedBudget = NonNullable<BudgetMonthResponse["budget"]>;

export type BudgetModalSources = {
  // useCategories()'s live, non-archived list: a template must never prefill
  // a cap onto a category the household archived (BudgetPage's own comment).
  categories: Category[];
  // This month's saved budget, or null in the empty state.
  budget: SavedBudget | null;
  // The previous month's saved budget, or null when it has none.
  prevMonthBudget: SavedBudget | null;
};

// Returns null when there is nothing to open: Edit budget with no saved
// budget, or Import last month when last month has none. Neither button
// renders in that state today, but the guard stays rather than a non-null
// assertion -- the same "fail closed on a value you did not just construct"
// instinct as everywhere else on this screen.
export function resolveTemplatePrefill(
  entry: BudgetModalEntry,
  sources: BudgetModalSources,
): BudgetModalState | null {
  switch (entry) {
    case "blank":
      return { prefill: null, awaitingIncome: false };
    case "familyOfFour":
      return { prefill: familyOfFourTemplate(sources.categories), awaitingIncome: false };
    case "fiftyThirtyTwenty":
      // Called with 0, not the previous month's or any guessed income --
      // fiftyThirtyTwentyTemplate treats that as "blank" and returns zero
      // lines (budgetTemplates.ts's own comment), which is exactly the
      // waiting-for-income state the modal opens into.
      return { prefill: fiftyThirtyTwentyTemplate(sources.categories, 0), awaitingIncome: true };
    case "editBudget":
      // An *existing* budget normalises into the same TemplatePrefill shape a
      // template produces, as BudgetModal.tsx's own header comment anticipated.
      if (!sources.budget) return null;
      return {
        prefill: {
          expectedIncomeMinor: sources.budget.expectedIncomeMinor,
          lines: sources.budget.lines,
          missing: [],
        },
        awaitingIncome: false,
      };
    case "importLastMonth":
      // The previous month's lines already reference real categoryIds -- no
      // name-mapping needed the way the two templates need it, so `missing`
      // is trivially empty here.
      if (!sources.prevMonthBudget) return null;
      return {
        prefill: {
          expectedIncomeMinor: sources.prevMonthBudget.expectedIncomeMinor,
          lines: sources.prevMonthBudget.lines,
          missing: [],
        },
        awaitingIncome: false,
      };
  }
}
