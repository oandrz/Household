// Tiny placeholder so router.tsx has a real component to mount at
// /money/budget -- Task 12 replaces this with the actual set-state screen
// (stat cards, categories grid, spending by person). The testid stays
// `budget-page` across that replacement so router.test.tsx's positive
// "reaches /money/budget" assertion doesn't need to change with it.
export function BudgetPage() {
  return <div data-testid="budget-page" />;
}
