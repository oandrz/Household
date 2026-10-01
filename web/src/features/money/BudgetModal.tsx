// The Edit-budget modal (design spec's "Edit budget modal" section):
// expected income, a live Allocated/Left-to-allocate pair, one row per
// capped category, and an "+ Add a category" control that can create a
// brand-new category, restore an archived one, or pick an already-active
// one that has no cap yet this month. Follows TransactionModal.tsx's shape
// (components/Modal, the shared money-input helpers from formatMoney.ts,
// field-error-then-mutation-error ordering) but -- unlike TransactionModal,
// which is handed its mutation as a prop -- this component calls
// `useBudget(month)` itself. That is what the props list (`month`, not a
// bound `onSubmit`) implies, and it is what makes `budget.data.currency`
// available here without a seventh prop: BudgetPage only ever opens this
// modal after its own `useBudget(month)` call has already resolved, so this
// second call to the same `["budget", month]` queryKey reads the warm cache
// instead of firing a second request.
//
// The rows and the add-a-category control live in useBudgetRows.ts, and one
// row's markup is BudgetCategoryRow.tsx; this file loads, renders and saves.
//
// `initial` is always a `TemplatePrefill` shape, never `null` and never a
// bare `{expectedIncomeMinor, lines}` -- BudgetPage.tsx normalises "Create
// your first budget" to `{expectedIncomeMinor: null, lines: [], missing:
// []}` before opening this modal, and a future "Edit budget" entry point
// (Task 15) for an *existing* budget would normalise the same way (`missing:
// []`, since nothing is missing from a template when there was no template).
// One shape in, rather than a union this component would have to re-branch
// on internally, for no behavioural difference either way.
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch, ApiError } from "../../api/client";
import { apiErrorMessage } from "../../api/errorMessage";
import { Field } from "../../components/Field";
import { FIELD_CONTROL_CLASS } from "../../components/fieldClasses";
import { Modal } from "../../components/Modal";
import { ModalActions } from "../../components/ModalActions";
import { BudgetCategoryRow } from "./BudgetCategoryRow";
import { BUDGET_COPY } from "./budgetCopy";
import { categorySchema as fullCategorySchema } from "./budgetSchemas";
import type { TemplatePrefill } from "./budgetTemplates";
import { formatMoney, minorUnitsToInputValue, toMinorUnits } from "./formatMoney";
import { useBudget, type SaveBudgetBody } from "./useBudget";
import { useBudgetRows, type BudgetRow, type CategoryOption } from "./useBudgetRows";

// GET /api/v1/categories?includeArchived=true's shape -- distinct from
// transactionSchemas.ts's `categoriesResponseSchema`, whose own
// `categorySchema` has no `archived` field and would silently strip it
// (budgetSchemas.ts's own comment on why that file redefines
// `categorySchema` rather than reusing transactionSchemas.ts's).
const categoriesWithArchivedResponseSchema = z.object({
  categories: z.array(fullCategorySchema),
});

export function BudgetModal({
  month,
  initial,
  categories,
  // Not in the brief's literal 5-prop list, but load-bearing:
  // budgetModalPrefill.ts's own `BudgetModalState` comment is explicit that
  // the 50/30/20 waiting-for-income state cannot be told apart from "this
  // household has zero matching categories" by `initial.lines.length === 0`
  // alone. Both are real, both start this modal with zero rows, and only this
  // flag says which one it is.
  awaitingIncome,
  onClose,
  onSaved,
}: {
  month: string;
  initial: TemplatePrefill;
  categories: CategoryOption[];
  awaitingIncome: boolean;
  onClose: () => void;
  onSaved: () => void;
}) {
  const budget = useBudget(month);
  // The household's real, include-archived category roster -- fetched here,
  // one level up from BudgetModalForm, so it is ready (not still in flight)
  // the moment that component's rows first build. Two callers need it once
  // it lands: buildRows in useBudgetRows.ts (Defect A, Task 17's browser walk
  // -- an archived category's line was rendering "Unknown category" because
  // name resolution ran off the active-only `categories` prop) and the
  // archived-name gotcha check in addCategoryByName (ledgered from Task 13's
  // review). `categories` (the prop) stays active-only throughout and is
  // still what the add-dropdown filters against -- that list must never
  // offer an archived category to pick again.
  //
  // The key is not categoriesQueryKey() but still starts with ["categories"],
  // so useBudget.ts's invalidateQueries({ queryKey: categoriesQueryKey() })
  // refreshes it through TanStack Query's prefix match. An `exact: true`
  // invalidation of that key would leave this one stale.
  const archivedAwareCategories = useQuery({
    queryKey: ["categories", { includeArchived: true }] as const,
    queryFn: async () => {
      const raw = await apiFetch<unknown>("/api/v1/categories?includeArchived=true");
      return categoriesWithArchivedResponseSchema.parse(raw).categories;
    },
  });
  // Falls back to the active-only prop on a genuine fetch failure rather
  // than hanging this modal on "Loading…" forever -- react-query gives up
  // retrying eventually, and a failed archived-inclusive fetch must not turn
  // into a modal a household can never open. That fallback reproduces
  // today's pre-fix behaviour (the `cat-gone` unresolved-line test below)
  // rather than a new failure mode.
  const allCategories = archivedAwareCategories.data ?? (archivedAwareCategories.isError ? categories : null);

  if (!budget.data || !allCategories) {
    return (
      <Modal open onClose={onClose} title={BUDGET_COPY.editBudget}>
        <p className="text-xs text-muted" data-testid="budget-modal-loading">
          Loading…
        </p>
      </Modal>
    );
  }

  return (
    <BudgetModalForm
      initial={initial}
      categories={categories}
      allCategories={allCategories}
      awaitingIncome={awaitingIncome}
      currency={budget.data.currency}
      onClose={onClose}
      onSaved={onSaved}
      save={budget.save}
      createCategory={budget.createCategory}
      renameCategory={budget.renameCategory}
      archiveCategory={budget.archiveCategory}
      restoreCategory={budget.restoreCategory}
    />
  );
}

// Split from `BudgetModal` so every field's `useState(() => ...)` initialiser
// -- which reads `initial`/`currency`, including useBudgetRows' own rows --
// runs exactly once, the moment `budget.data` first exists. `BudgetModal`
// re-rendering later (a background refetch after `budget.save`'s own
// invalidation, before `onClose` unmounts everything) does not remount this
// component -- same type, same position in the tree -- so a household's
// in-progress edits are never silently reseeded from a fresher server
// response mid-session.
function BudgetModalForm({
  initial,
  categories,
  allCategories,
  awaitingIncome,
  currency,
  onClose,
  onSaved,
  save,
  createCategory,
  renameCategory,
  archiveCategory,
  restoreCategory,
}: {
  initial: TemplatePrefill;
  categories: CategoryOption[];
  allCategories: CategoryOption[];
  awaitingIncome: boolean;
  currency: string;
  onClose: () => void;
  onSaved: () => void;
  save: (body: SaveBudgetBody) => Promise<void>;
  createCategory: ReturnType<typeof useBudget>["createCategory"];
  renameCategory: ReturnType<typeof useBudget>["renameCategory"];
  archiveCategory: ReturnType<typeof useBudget>["archiveCategory"];
  restoreCategory: ReturnType<typeof useBudget>["restoreCategory"];
}) {
  const {
    rows,
    missingToShow,
    availableToAdd,
    addSelectValue,
    newCategoryName,
    setNewCategoryName,
    addCategoryError,
    addCategoryByName,
    prefillFromIncome,
    removeRow,
    toggleArchiveRow,
    renameRow,
    capRow,
    handleAddSelectChange,
    handleAddNewCategory,
  } = useBudgetRows({ initial, categories, allCategories, awaitingIncome, currency });
  const [incomeInput, setIncomeInput] = useState(() =>
    initial.expectedIncomeMinor != null ? minorUnitsToInputValue(initial.expectedIncomeMinor, currency) : "",
  );
  const [saveError, setSaveError] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);

  function handleIncomeChange(value: string) {
    setIncomeInput(value);
    // A no-op unless the 50/30/20 template is waiting on income -- see
    // prefillFromIncome's own comment.
    prefillFromIncome(value);
  }

  // Live figures, tolerant of an unparsable or blank cap (treated as 0 for
  // display only -- Save below refuses to fire anything on a genuinely bad
  // value rather than silently submitting a guessed 0).
  const allocatedMinor = rows.reduce((sum, row) => {
    if (row.capInput.trim() === "") return sum;
    return sum + (toMinorUnits(row.capInput, currency) ?? 0);
  }, 0);
  const incomeBlank = incomeInput.trim() === "";
  const incomeForDisplay = incomeBlank ? null : (toMinorUnits(incomeInput, currency) ?? 0);
  const leftToAllocateMinor = incomeForDisplay === null ? null : incomeForDisplay - allocatedMinor;

  async function handleSave() {
    setSaveError(null);

    let incomeMinor: number | null = null;
    if (!incomeBlank) {
      const parsed = toMinorUnits(incomeInput, currency);
      if (parsed === null || parsed < 0) {
        setSaveError("Enter income as a number, or leave it blank.");
        return;
      }
      incomeMinor = parsed;
    }

    // Every row is checked before anything is sent, and each row is paired
    // here with the cap it will save -- so the write loop below reads the cap
    // straight off the pair instead of looking it up again by key.
    const rowsWithCaps: { row: BudgetRow; capMinor: number }[] = [];
    for (const row of rows) {
      if (!row.name.trim()) {
        setSaveError("Every category needs a name.");
        return;
      }
      const parsedCap = row.capInput.trim() === "" ? 0 : toMinorUnits(row.capInput, currency);
      if (parsedCap === null || parsedCap < 0) {
        setSaveError(`Enter a cap for "${row.name}" as a number, or leave it blank for 0.`);
        return;
      }
      rowsWithCaps.push({ row, capMinor: parsedCap });
    }

    setIsSaving(true);
    // Named outside the try so the 409 branch below can echo back whichever
    // create/rename was in flight when the server refused it -- the server's
    // own CATEGORY_NAME_TAKEN message never carries the name (see
    // budgetCopy.ts's categoryNameTaken comment).
    let attemptedName = "";
    try {
      const lines: SaveBudgetBody["lines"] = [];
      // Sequential, not Promise.all: the spec requires every queued create,
      // rename and archive to run to completion, in order, before the PUT --
      // and to stop at the first failure without firing the rest. A
      // concurrent Promise.all would race an unrelated later row's write
      // ahead of an earlier one that was about to fail, and would still fire
      // every row's call even when an early one 409s.
      for (const { row, capMinor } of rowsWithCaps) {
        let categoryId: string;
        if (row.action === "create") {
          attemptedName = row.name.trim();
          const created = await createCategory(attemptedName);
          categoryId = created.id;
        } else {
          // Any row not waiting on a create already carries its real id --
          // BudgetRow's own type guarantees it, so there is nothing to assert.
          categoryId = row.categoryId;
          if (row.action === "restore") {
            attemptedName = row.name.trim();
            await restoreCategory(categoryId);
          }
          if (row.name.trim() !== row.originalName) {
            attemptedName = row.name.trim();
            await renameCategory(categoryId, attemptedName);
          }
        }
        if (row.queuedArchive) {
          await archiveCategory(categoryId);
        }
        lines.push({ categoryId, capMinor });
      }

      await save({ expectedIncomeMinor: incomeMinor, lines });
      onSaved();
      onClose();
    } catch (err) {
      if (err instanceof ApiError && err.code === "CATEGORY_NAME_TAKEN") {
        setSaveError(BUDGET_COPY.categoryNameTaken(attemptedName));
      } else {
        setSaveError(apiErrorMessage(err, "Something went wrong. Please try again."));
      }
    } finally {
      setIsSaving(false);
    }
  }

  return (
    <Modal open onClose={onClose} title={BUDGET_COPY.editBudget}>
      <div className="flex flex-col gap-4">
        <Field label={BUDGET_COPY.expectedIncome} htmlFor="budget-modal-income">
          <input
            id="budget-modal-income"
            type="text"
            inputMode="decimal"
            autoFocus={awaitingIncome}
            value={incomeInput}
            onChange={(event) => handleIncomeChange(event.target.value)}
            className={FIELD_CONTROL_CLASS}
          />
        </Field>

        {awaitingIncome && incomeBlank && (
          <p className="text-xs text-muted" data-testid="budget-modal-fifty-thirty-twenty-prompt">
            {BUDGET_COPY.fiftyThirtyTwentyPrompt}
          </p>
        )}

        {!incomeBlank && (
          <div className="flex gap-4">
            <div data-testid="budget-modal-allocated" className="text-[13px] text-ink">
              {BUDGET_COPY.allocated}: {formatMoney(allocatedMinor, currency)}
            </div>
            <div data-testid="budget-modal-left-to-allocate" className="text-[13px] text-ink">
              {BUDGET_COPY.leftToAllocate}: {formatMoney(leftToAllocateMinor ?? 0, currency)}
            </div>
          </div>
        )}

        {missingToShow.length > 0 && (
          <div data-testid="budget-modal-missing" className="flex flex-col gap-1.5">
            <p className="text-xs font-semibold text-label">{BUDGET_COPY.suggestedByTemplate}</p>
            {missingToShow.map((name) => (
              <div key={name} className="flex items-center justify-between gap-2">
                <span className="text-[13px] text-ink">{name}</span>
                <button
                  type="button"
                  onClick={() => addCategoryByName(name)}
                  className="min-h-11 text-[12.5px] font-semibold text-accent sm:min-h-0"
                >
                  {BUDGET_COPY.addCategory}
                </button>
              </div>
            ))}
          </div>
        )}

        <div className="flex flex-col gap-3">
          {rows.map((row) => (
            <BudgetCategoryRow
              key={row.key}
              row={row}
              onRename={(name) => renameRow(row.key, name)}
              onCapChange={(capInput) => capRow(row.key, capInput)}
              onToggleArchive={() => toggleArchiveRow(row.key)}
              onRemove={() => removeRow(row.key)}
            />
          ))}
        </div>

        <Field
          label={BUDGET_COPY.addACategory}
          htmlFor="budget-modal-add-select"
          error={addCategoryError}
          errorTestId="budget-modal-add-category-error"
        >
          <select
            id="budget-modal-add-select"
            value={addSelectValue}
            onChange={(event) => handleAddSelectChange(event.target.value)}
            className={FIELD_CONTROL_CLASS}
          >
            <option value="">{BUDGET_COPY.chooseACategory}</option>
            {availableToAdd.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
            <option value="__new__">{BUDGET_COPY.newCategoryOption}</option>
          </select>
          {addSelectValue === "__new__" && (
            <div className="flex gap-2" data-testid="budget-modal-new-category-form">
              <label htmlFor="budget-modal-new-category-name" className="sr-only">
                {BUDGET_COPY.newCategoryName}
              </label>
              <input
                id="budget-modal-new-category-name"
                type="text"
                value={newCategoryName}
                onChange={(event) => setNewCategoryName(event.target.value)}
                className={`${FIELD_CONTROL_CLASS} flex-1`}
              />
              <button
                type="button"
                onClick={handleAddNewCategory}
                className="min-h-11 rounded-lg border border-hairline px-3.5 py-2.5 text-[13px] font-semibold text-label sm:min-h-0"
              >
                {BUDGET_COPY.addCategory}
              </button>
            </div>
          )}
        </Field>

        {saveError !== null && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {saveError}
          </p>
        )}

        {/* primaryType="button": there is no <form> here at all -- Save runs
            only from its own click. */}
        <ModalActions
          secondaryLabel={BUDGET_COPY.cancel}
          onSecondary={onClose}
          primaryLabel={BUDGET_COPY.saveBudget}
          primaryType="button"
          onPrimary={() => void handleSave()}
          primaryDisabled={isSaving}
        />
      </div>
    </Modal>
  );
}
