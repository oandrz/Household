import { describe, expect, it } from "vitest";
import { resolveTemplatePrefill, type BudgetModalSources } from "./budgetModalPrefill";
import { familyOfFourTemplate, fiftyThirtyTwentyTemplate } from "./budgetTemplates";
import type { Category } from "./transactionSchemas";

const categories: Category[] = [
  { id: "c-groceries", name: "Groceries", kind: "expense" },
  { id: "c-dining", name: "Dining out", kind: "expense" },
];

const julyBudget: NonNullable<BudgetModalSources["budget"]> = {
  expectedIncomeMinor: 900000,
  lines: [{ categoryId: "c-groceries", capMinor: 80000 }],
};

const juneBudget: NonNullable<BudgetModalSources["budget"]> = {
  expectedIncomeMinor: 850000,
  lines: [{ categoryId: "c-dining", capMinor: 45000 }],
};

const emptyMonth: BudgetModalSources = { categories, budget: null, prevMonthBudget: null };

describe("resolveTemplatePrefill", () => {
  it("opens a blank budget with no prefill and no income prompt", () => {
    expect(resolveTemplatePrefill("blank", emptyMonth)).toEqual({
      prefill: null,
      awaitingIncome: false,
    });
  });

  it("prefills Family of four from the household's live categories", () => {
    expect(resolveTemplatePrefill("familyOfFour", emptyMonth)).toEqual({
      prefill: familyOfFourTemplate(categories),
      awaitingIncome: false,
    });
  });

  it("opens 50/30/20 waiting for an income, since its lines depend on one", () => {
    expect(resolveTemplatePrefill("fiftyThirtyTwenty", emptyMonth)).toEqual({
      prefill: fiftyThirtyTwentyTemplate(categories, 0),
      awaitingIncome: true,
    });
  });

  it("edits this month's saved budget, and opens nothing when there is none", () => {
    expect(resolveTemplatePrefill("editBudget", { ...emptyMonth, budget: julyBudget })).toEqual({
      prefill: { expectedIncomeMinor: 900000, lines: julyBudget.lines, missing: [] },
      awaitingIncome: false,
    });
    expect(resolveTemplatePrefill("editBudget", emptyMonth)).toBeNull();
  });

  it("imports last month's caps and income, and opens nothing when last month has no budget", () => {
    expect(
      resolveTemplatePrefill("importLastMonth", { ...emptyMonth, prevMonthBudget: juneBudget }),
    ).toEqual({
      prefill: { expectedIncomeMinor: 850000, lines: juneBudget.lines, missing: [] },
      awaitingIncome: false,
    });
    expect(resolveTemplatePrefill("importLastMonth", emptyMonth)).toBeNull();
  });
});
