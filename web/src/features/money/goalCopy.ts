// Copy for the Goals screen, in a plain .ts module for the same reason
// budgetCopy.ts/transactionCopy.ts are -- eslint's
// react-refresh/only-export-components rule never has to think about a file
// that mixes components with other exports.
//
// Task 10 only needs a page title: router.tsx's temporary placeholder at
// /money/goals (Task 11 replaces it with the real GoalsPage) renders this as
// its heading, so the file has a real reader from the moment it exists
// rather than sitting unused until Task 11. The rest of the screen's copy
// grows here task by task the same way BUDGET_COPY did.
export const GOAL_COPY = {
  title: "Goals",
} as const;
