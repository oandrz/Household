import { describe, expect, it } from "vitest";
import { familyOfFourTemplate, fiftyThirtyTwentyTemplate } from "./budgetTemplates";
import type { Category } from "./transactionSchemas";

function category(id: string, name: string): Category {
  return { id, name, kind: "expense" };
}

// A household whose category names cover every template name this file
// tests against, so `missing` stays empty unless a test deliberately
// removes one -- the "found" path and the "missing" path get separate
// tests below rather than one fixture doing both jobs at once.
const ALL_NAMED: Category[] = [
  category("c-groceries", "Groceries"),
  category("c-dining", "Dining out"),
  category("c-kids", "Kids & school"),
  category("c-insurance", "Insurance"),
  category("c-utilities", "Utilities"),
  category("c-transport", "Transport"),
  category("c-petrol", "Petrol"),
  category("c-household", "Household"),
  category("c-giving", "Giving"),
  category("c-fun", "Fun & hobbies"),
];

describe("familyOfFourTemplate", () => {
  it("maps all ten design caps onto matching categories by name", () => {
    const prefill = familyOfFourTemplate(ALL_NAMED);

    expect(prefill.expectedIncomeMinor).toBeNull();
    expect(prefill.lines).toHaveLength(10);
    expect(prefill.missing).toEqual([]);

    const groceries = prefill.lines.find((line) => line.categoryId === "c-groceries");
    expect(groceries?.capMinor).toBe(80000);
    const dining = prefill.lines.find((line) => line.categoryId === "c-dining");
    expect(dining?.capMinor).toBe(45000);
  });

  it("never invents an id: a template name with no live category lands in `missing`", () => {
    const withoutDining = ALL_NAMED.filter((c) => c.name !== "Dining out");

    const prefill = familyOfFourTemplate(withoutDining);

    expect(prefill.missing).toEqual(["Dining out"]);
    expect(prefill.lines.some((line) => line.capMinor === 45000)).toBe(false);
    expect(prefill.lines).toHaveLength(9);
  });

  it("matches only an exact name, not a case-insensitive or partial one", () => {
    const renamed = ALL_NAMED.map((c) =>
      c.name === "Groceries" ? { ...c, name: "groceries" } : c,
    );

    const prefill = familyOfFourTemplate(renamed);

    expect(prefill.missing).toContain("Groceries");
  });

  it("ignores a household category with no matching template name", () => {
    const withExtra = [...ALL_NAMED, category("c-pets", "Pet care")];

    const prefill = familyOfFourTemplate(withExtra);

    expect(prefill.lines).toHaveLength(10);
    expect(prefill.lines.some((line) => line.categoryId === "c-pets")).toBe(false);
  });
});

describe("fiftyThirtyTwentyTemplate", () => {
  // The waiting-for-income state: BudgetPage.tsx calls this the instant the
  // 50/30/20 card is clicked, before any income figure exists.
  it("returns no lines while income is 0 (blank)", () => {
    const prefill = fiftyThirtyTwentyTemplate(ALL_NAMED, 0);

    expect(prefill.lines).toEqual([]);
    expect(prefill.expectedIncomeMinor).toBeNull();
  });

  it("splits income 50/30/20, leaving the line sum at or under 80% (20% headroom)", () => {
    const incomeMinor = 500000; // S$5,000.00
    const prefill = fiftyThirtyTwentyTemplate(ALL_NAMED, incomeMinor);

    const sum = prefill.lines.reduce((total, line) => total + line.capMinor, 0);
    expect(sum).toBeLessThanOrEqual(incomeMinor);
    expect(sum).toBeLessThanOrEqual(Math.floor(incomeMinor * 0.8));
    expect(prefill.expectedIncomeMinor).toBe(incomeMinor);
  });

  it("floors every line so the split can never exceed income, even on an awkward figure", () => {
    // 333333 does not divide cleanly by any of the family-of-four weights --
    // exactly the case flooring exists for.
    const incomeMinor = 333333;
    const prefill = fiftyThirtyTwentyTemplate(ALL_NAMED, incomeMinor);

    const sum = prefill.lines.reduce((total, line) => total + line.capMinor, 0);
    expect(sum).toBeLessThanOrEqual(incomeMinor);
    for (const line of prefill.lines) {
      expect(Number.isInteger(line.capMinor)).toBe(true);
    }
  });

  it("puts a needs/wants name with no live category into `missing`", () => {
    const withoutGroceries = ALL_NAMED.filter((c) => c.name !== "Groceries");

    const prefill = fiftyThirtyTwentyTemplate(withoutGroceries, 500000);

    expect(prefill.missing).toContain("Groceries");
  });

  // "Health" is named in the needs set but has no family-of-four weight, so
  // it never gets a line -- and, since it was never going to get one, a
  // household that happens to have a live "Health" category is not told
  // it's missing either.
  it("funds nothing for a needs name with no known weight, and does not report it as missing", () => {
    const withHealth = [...ALL_NAMED, category("c-health", "Health")];

    const prefill = fiftyThirtyTwentyTemplate(withHealth, 500000);

    expect(prefill.lines.some((line) => line.categoryId === "c-health")).toBe(false);
    expect(prefill.missing).not.toContain("Health");
  });

  it("splits proportionally to the family-of-four weights within the needs pool", () => {
    // Groceries (80000) is twice Utilities (32000)... no -- pick two whose
    // ratio is exact: Household and Petrol are both 25000 (equal weight),
    // so their prefilled caps must land equal.
    const prefill = fiftyThirtyTwentyTemplate(ALL_NAMED, 1000000);

    const household = prefill.lines.find((l) => l.categoryId === "c-household");
    const petrol = prefill.lines.find((l) => l.categoryId === "c-petrol");
    expect(household?.capMinor).toBe(petrol?.capMinor);
    expect(household?.capMinor).toBeGreaterThan(0);
  });
});
