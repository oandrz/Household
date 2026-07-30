// Pure prefill computation for the Budget empty state's two templates
// (BudgetPage.tsx's "Family of four" and "50 / 30 / 20" cards). Nothing
// here fetches or writes -- spec decision 6: "Templates prefill the modal;
// they never write directly." Every function takes the household's real,
// already-fetched category list and returns a `TemplatePrefill` for the
// (Task 14) modal to render; the household still reviews and clicks Save.
//
// Categories are matched **by name**, never invented: a template names a
// job ("Groceries", "Dining out"...) and looks for a live category with
// that exact name. A household without a matching category doesn't get a
// guessed id -- the name lands in `missing` instead, so the modal can offer
// it as a one-click "add this category" suggestion (Task 14) rather than
// silently dropping the line or fabricating an id nothing on the server
// recognises.
import type { Category } from "./transactionSchemas";

export type TemplatePrefill = {
  expectedIncomeMinor: number | null;
  lines: { categoryId: string; capMinor: number }[];
  missing: string[];
};

// The design's own Budget screen numbers (Household Dashboard.dc.html),
// carried here as primary-currency minor units -- the "10 categories" the
// empty state's Family-of-four card advertises. This table is also the
// weight 50/30/20 (below) splits proportionally from: a needs/wants name
// with no entry here (only "Health" today -- see NEEDS's own comment)
// simply has no seat at this table, i.e. weight zero, not a crash or a
// guessed number.
const FAMILY_OF_FOUR_CAPS: Record<string, number> = {
  Groceries: 80000,
  "Dining out": 45000,
  "Kids & school": 60000,
  Insurance: 42000,
  Utilities: 32000,
  Transport: 30000,
  Petrol: 25000,
  Household: 25000,
  Giving: 20000,
  "Fun & hobbies": 20000,
};

// The needs/wants sets 50/30/20 splits its two pools across. "Health" is
// deliberately in NEEDS despite having no entry in FAMILY_OF_FOUR_CAPS --
// the brief names it as part of the needs set anyway, and splitPool's own
// weight-zero guard means it is simply never funded from either pool (no
// line, and not reported as `missing` either, since a household that does
// have a live "Health" category still gets nothing wrong said about it --
// it just isn't one of the ten categories this template knows a starter
// number for).
const NEEDS = [
  "Groceries",
  "Utilities",
  "Transport",
  "Insurance",
  "Kids & school",
  "Household",
  "Petrol",
  "Health",
];
const WANTS = ["Dining out", "Fun & hobbies", "Giving"];

function findByName(categories: Category[], name: string): Category | undefined {
  return categories.find((category) => category.name === name);
}

// familyOfFourTemplate assigns the design's ten starter caps directly, one
// per matching live category -- no income needed, no proportional split,
// just "here is a round number to start from."
export function familyOfFourTemplate(categories: Category[]): TemplatePrefill {
  const lines: TemplatePrefill["lines"] = [];
  const missing: string[] = [];

  for (const [name, capMinor] of Object.entries(FAMILY_OF_FOUR_CAPS)) {
    const category = findByName(categories, name);
    if (category) {
      lines.push({ categoryId: category.id, capMinor });
    } else {
      missing.push(name);
    }
  }

  return { expectedIncomeMinor: null, lines, missing };
}

// splitPool divides one pool of money (50% or 30% of income) across `names`
// proportionally to their FAMILY_OF_FOUR_CAPS weight, flooring every line to
// a whole minor unit. Flooring (never rounding) is what keeps the promise
// "the split never exceeds income": summing floored shares of a pool can
// only come in at or under that pool, never over it, regardless of how the
// weights divide.
function splitPool(
  names: string[],
  categories: Category[],
  poolMinor: number,
): { lines: TemplatePrefill["lines"]; missing: string[] } {
  const weights = names.map((name) => FAMILY_OF_FOUR_CAPS[name] ?? 0);
  const totalWeight = weights.reduce((sum, weight) => sum + weight, 0);

  const lines: TemplatePrefill["lines"] = [];
  const missing: string[] = [];
  if (totalWeight <= 0) {
    return { lines, missing };
  }

  names.forEach((name, index) => {
    const weight = weights[index];
    // No weight, no job to fund from this pool -- see NEEDS's own comment
    // on why "Health" always takes this branch today.
    if (weight <= 0) return;
    const capMinor = Math.floor((poolMinor * weight) / totalWeight);
    if (capMinor <= 0) return;
    const category = findByName(categories, name);
    if (category) {
      lines.push({ categoryId: category.id, capMinor });
    } else {
      missing.push(name);
    }
  });

  return { lines, missing };
}

// fiftyThirtyTwentyTemplate splits `incomeMinor` 50% needs / 30% wants,
// leaving 20% as unallocated savings headroom (spec decision 6). It is the
// one template that cannot prefill without income: BudgetPage.tsx calls
// this with `0` the instant the card is clicked (income not typed yet),
// which this function treats identically to "blank" -- no pool to split, so
// no lines -- and recomputes once a real income figure is entered. That is
// the "waiting for income" state the modal (Task 14) renders around.
export function fiftyThirtyTwentyTemplate(
  categories: Category[],
  incomeMinor: number,
): TemplatePrefill {
  if (!incomeMinor || incomeMinor <= 0) {
    return { expectedIncomeMinor: incomeMinor || null, lines: [], missing: [] };
  }

  const needs = splitPool(NEEDS, categories, incomeMinor * 0.5);
  const wants = splitPool(WANTS, categories, incomeMinor * 0.3);

  return {
    expectedIncomeMinor: incomeMinor,
    lines: [...needs.lines, ...wants.lines],
    missing: [...needs.missing, ...wants.missing],
  };
}
