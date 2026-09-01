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
// `initial` is always a `TemplatePrefill` shape, never `null` and never a
// bare `{expectedIncomeMinor, lines}` -- BudgetPage.tsx normalises "Create
// your first budget" to `{expectedIncomeMinor: null, lines: [], missing:
// []}` before opening this modal, and a future "Edit budget" entry point
// (Task 15) for an *existing* budget would normalise the same way (`missing:
// []`, since nothing is missing from a template when there was no template).
// One shape in, rather than a union this component would have to re-branch
// on internally, for no behavioural difference either way.
import { useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch, ApiError } from "../../api/client";
import { Modal } from "../../components/Modal";
import { CloseIcon } from "../../components/icons";
import { apiErrorMessage } from "../auth/copy";
import { BUDGET_COPY } from "./budgetCopy";
import { categorySchema as fullCategorySchema } from "./budgetSchemas";
import { fiftyThirtyTwentyTemplate, type TemplatePrefill } from "./budgetTemplates";
import { formatMoney, minorUnitsToInputValue, toMinorUnits } from "./formatMoney";
import { useBudget, type SaveBudgetBody } from "./useBudget";

// The household's plain category list (BudgetPage.tsx's `useCategories()`
// data) -- active only, by construction of that endpoint's default
// `includeArchived=false`. `archived` is typed optional here rather than
// omitted: `kind` is required by both `Category` shapes this component is
// ever handed (transactionSchemas.ts's and budgetSchemas.ts's), but a
// defensive `!c.archived` check on the add-category dropdown costs nothing
// and guards the shape this prop is documented, not enforced, to hold.
type CategoryOption = {
  id: string;
  name: string;
  kind: "expense" | "income";
  archived?: boolean;
};

// GET /api/v1/categories?includeArchived=true's shape -- distinct from
// transactionSchemas.ts's `categoriesResponseSchema`, whose own
// `categorySchema` has no `archived` field and would silently strip it
// (budgetSchemas.ts's own comment on why that file redefines
// `categorySchema` rather than reusing transactionSchemas.ts's).
const categoriesWithArchivedResponseSchema = z.object({
  categories: z.array(fullCategorySchema),
});

type RowAction = "create" | "restore" | null;

// One row per capped category in the modal. `key` is stable across
// re-renders regardless of `action`: a real category's own id for an
// existing/restored/dropdown-picked row, or a locally-generated temp id for
// a row still waiting on its create call.
type Row = {
  key: string;
  categoryId: string | null;
  name: string;
  // "" for a row with nothing to compare a rename against (a brand-new
  // create, or a restore whose name at match time IS what will be sent --
  // see buildRows and addCategoryByName). A real row's original name, once
  // known, never becomes "" again, so `name.trim() !== originalName` stays
  // a safe rename test for the lifetime of the row.
  originalName: string;
  capInput: string;
  archived: boolean;
  queuedArchive: boolean;
  action: RowAction;
};

function buildRows(
  lines: TemplatePrefill["lines"],
  categories: CategoryOption[],
  currency: string,
): Row[] {
  return lines.map((line) => {
    const found = categories.find((c) => c.id === line.categoryId);
    // A line whose category this modal cannot name at all: there is no hard
    // delete, so this is only reachable if `categories` (whatever list the
    // caller handed in) somehow still doesn't carry the id -- e.g. a
    // household deleted between the archived-inclusive fetch resolving and
    // this render, or a genuinely stale id from "Import last month"'s
    // straight-through handoff of `prevMonthBudget.lines`.
    // `name` and `originalName` MUST be the same fallback string, not two
    // different ones ("Unknown category" vs "") -- Save's rename check is
    // `row.name.trim() !== row.originalName`, and two different fallbacks
    // would make that true unconditionally, firing a PATCH that silently
    // renames a real (possibly archived) category to "Unknown category" on
    // every save. Falling back to the same string on both sides is what
    // keeps that comparison honest: still renders, still submits its real
    // id and cap unchanged, but queues no rename nobody asked for.
    const name = found?.name ?? "Unknown category";
    return {
      key: line.categoryId,
      categoryId: line.categoryId,
      name,
      originalName: name,
      capInput: minorUnitsToInputValue(line.capMinor, currency),
      archived: Boolean(found?.archived),
      queuedArchive: false,
      action: null,
    };
  });
}

export function BudgetModal({
  month,
  initial,
  categories,
  // Not in the brief's literal 5-prop list, but load-bearing: BudgetPage.tsx's
  // own `ModalState` comment is explicit that the 50/30/20 waiting-for-income
  // state cannot be told apart from "this household has zero matching
  // categories" by `initial.lines.length === 0` alone. Both are real, both
  // start this modal with zero rows, and only this flag says which one it is.
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
  // it lands: buildRows below (Defect A, Task 17's browser walk -- an
  // archived category's line was rendering "Unknown category" because name
  // resolution ran off the active-only `categories` prop) and the
  // archived-name gotcha check in addCategoryByName (ledgered from Task 13's
  // review). `categories` (the prop) stays active-only throughout and is
  // still what the add-dropdown filters against -- that list must never
  // offer an archived category to pick again.
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
// -- which reads `initial`/`currency` -- runs exactly once, the moment
// `budget.data` first exists. `BudgetModal` re-rendering later (a background
// refetch after `budget.save`'s own invalidation, before `onClose` unmounts
// everything) does not remount this component -- same type, same position in
// the tree -- so a household's in-progress edits are never silently reseeded
// from a fresher server response mid-session.
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
  const [rows, setRows] = useState<Row[]>(() => buildRows(initial.lines, allCategories, currency));
  const [missing, setMissing] = useState<string[]>(initial.missing);
  const [incomeInput, setIncomeInput] = useState(() =>
    initial.expectedIncomeMinor != null ? minorUnitsToInputValue(initial.expectedIncomeMinor, currency) : "",
  );
  const [addSelectValue, setAddSelectValue] = useState("");
  const [newCategoryName, setNewCategoryName] = useState("");
  // Task 17 browser walk, Defect B: typing an already-used name into "New
  // category…" and clicking Add was a silent no-op -- the duplicate guard
  // below returned with no row added and no feedback at all. This surfaces
  // that guard as a visible inline error next to the add control, reusing
  // the 409 CATEGORY_NAME_TAKEN copy shape (categoryNameTaken below) since
  // it is naming the same fact: this household already has a category by
  // this name.
  const [addCategoryError, setAddCategoryError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const tempKeyRef = useRef(0);

  // Returns whether a row was actually added -- callers that reset the
  // "New category…" input on success (handleAddNewCategory below) need to
  // know the difference between "added" and "refused," since the taken-name
  // refusal below must leave the typed value in place for the household to
  // see what they typed and correct it.
  function addCategoryByName(rawName: string): boolean {
    const name = rawName.trim();
    if (!name) return false;
    if (rows.some((row) => row.name === name)) {
      setAddCategoryError(BUDGET_COPY.categoryNameTaken(name));
      return false;
    }
    setAddCategoryError(null);

    // The archived-name gotcha (ledgered from Task 13's review): a
    // template's `missing` name, or a name typed into "New category…",
    // might belong to a category this household already has -- just
    // archived. Creating it again 409s on
    // categories_household_id_name_key. `allCategories` is the household's
    // real, include-archived roster this checks against before deciding
    // whether "Add" means create or restore; `categories` (the prop) is
    // active-only by construction and cannot answer that question on its
    // own.
    const archivedMatch = allCategories.find((c) => c.name === name && c.archived);
    if (archivedMatch) {
      setRows((prev) => [
        ...prev,
        {
          key: archivedMatch.id,
          categoryId: archivedMatch.id,
          name: archivedMatch.name,
          originalName: archivedMatch.name,
          capInput: "",
          archived: true,
          queuedArchive: false,
          action: "restore",
        },
      ]);
      return true;
    }

    const activeMatch = categories.find((c) => c.name === name && !c.archived);
    if (activeMatch) {
      setRows((prev) => [
        ...prev,
        {
          key: activeMatch.id,
          categoryId: activeMatch.id,
          name: activeMatch.name,
          originalName: activeMatch.name,
          capInput: "",
          archived: false,
          queuedArchive: false,
          action: null,
        },
      ]);
      return true;
    }

    tempKeyRef.current += 1;
    setRows((prev) => [
      ...prev,
      {
        key: `new-${tempKeyRef.current}`,
        categoryId: null,
        name,
        originalName: "",
        capInput: "",
        archived: false,
        queuedArchive: false,
        action: "create",
      },
    ]);
    return true;
  }

  function handleIncomeChange(value: string) {
    setIncomeInput(value);
    // Only the 50/30/20 template's waiting-for-income state drives caps off
    // income -- every other opening (blank, family-of-four, an existing
    // budget) treats income as a plain editable field with no effect on the
    // rows below it. Recomputes on every keystroke, replacing whatever the
    // template last computed: a household that hand-tweaks a row and then
    // keeps typing into income loses that tweak, an accepted simplification
    // for a template's one-shot prefill role (this is never reachable for
    // the general edit flow, which never sets `awaitingIncome`).
    if (!awaitingIncome) return;
    const minor = value.trim() === "" ? null : toMinorUnits(value, currency);
    if (minor !== null && minor > 0) {
      const prefill = fiftyThirtyTwentyTemplate(categories, minor);
      setRows(buildRows(prefill.lines, categories, currency));
      setMissing(prefill.missing);
    } else {
      setRows([]);
      setMissing([]);
    }
  }

  function removeRow(key: string) {
    setRows((prev) => prev.filter((row) => row.key !== key));
  }

  function toggleArchiveRow(key: string) {
    setRows((prev) => prev.map((row) => (row.key === key ? { ...row, queuedArchive: !row.queuedArchive } : row)));
  }

  function renameRow(key: string, name: string) {
    setRows((prev) => prev.map((row) => (row.key === key ? { ...row, name } : row)));
  }

  function capRow(key: string, capInput: string) {
    setRows((prev) => prev.map((row) => (row.key === key ? { ...row, capInput } : row)));
  }

  function handleAddSelectChange(value: string) {
    setAddSelectValue(value);
    // Leaving "New category…" for something else hides the form the error
    // is attached to (gated on addSelectValue === "__new__" below) -- clear
    // it here too so a stale refusal from a previous attempt can't flash
    // back in if the household reopens "New category…" later.
    setAddCategoryError(null);
    if (value === "" || value === "__new__") return;
    const picked = categories.find((c) => c.id === value);
    if (!picked) return;
    setRows((prev) => [
      ...prev,
      {
        key: picked.id,
        categoryId: picked.id,
        name: picked.name,
        originalName: picked.name,
        capInput: "",
        archived: false,
        queuedArchive: false,
        action: null,
      },
    ]);
    setAddSelectValue("");
  }

  function handleAddNewCategory() {
    // Only clears the input and closes "New category…" on an actual add --
    // a taken-name refusal (addCategoryByName returning false, and setting
    // addCategoryError itself) must leave the typed value and the form
    // exactly as the household left them, per Defect B's fix.
    if (!addCategoryByName(newCategoryName)) return;
    setNewCategoryName("");
    setAddSelectValue("");
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

  const missingToShow = missing.filter((name) => !rows.some((row) => row.name === name));
  const availableToAdd = categories.filter(
    (c) => c.kind === "expense" && !c.archived && !rows.some((row) => row.categoryId === c.id),
  );

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

    const capsByKey = new Map<string, number>();
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
      capsByKey.set(row.key, parsedCap);
    }

    setIsSaving(true);
    // Named outside the try so the 409 branch below can echo back whichever
    // create/rename was in flight when the server refused it -- the server's
    // own CATEGORY_NAME_TAKEN message never carries the name (see
    // budgetCopy.ts's categoryNameTaken comment).
    let attemptedName = "";
    try {
      const resolvedIds = new Map<string, string>();
      // Sequential, not Promise.all: the spec requires every queued create,
      // rename and archive to run to completion, in order, before the PUT --
      // and to stop at the first failure without firing the rest. A
      // concurrent Promise.all would race an unrelated later row's write
      // ahead of an earlier one that was about to fail, and would still fire
      // every row's call even when an early one 409s.
      for (const row of rows) {
        let categoryId = row.categoryId;
        if (row.action === "create") {
          attemptedName = row.name.trim();
          const created = await createCategory(attemptedName);
          categoryId = created.id;
        } else {
          if (row.action === "restore") {
            attemptedName = row.name.trim();
            await restoreCategory(row.categoryId!);
          }
          if (categoryId && row.name.trim() !== row.originalName) {
            attemptedName = row.name.trim();
            await renameCategory(categoryId, attemptedName);
          }
        }
        if (row.queuedArchive && categoryId) {
          await archiveCategory(categoryId);
        }
        resolvedIds.set(row.key, categoryId!);
      }

      const body: SaveBudgetBody = {
        expectedIncomeMinor: incomeMinor,
        lines: rows.map((row) => ({
          categoryId: resolvedIds.get(row.key)!,
          capMinor: capsByKey.get(row.key)!,
        })),
      };
      await save(body);
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
        <div className="flex flex-col gap-1.5">
          <label htmlFor="budget-modal-income" className="text-xs font-semibold text-label">
            {BUDGET_COPY.expectedIncome}
          </label>
          <input
            id="budget-modal-income"
            type="text"
            inputMode="decimal"
            autoFocus={awaitingIncome}
            value={incomeInput}
            onChange={(event) => handleIncomeChange(event.target.value)}
            // min-h-11/sm:min-h-0 on every field in this modal:
            // TransactionFilters.tsx's own SELECT_CLASS comment has the
            // measured reason py-2.5 alone falls short of the 44px floor on
            // a phone.
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
          />
        </div>

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
            <div
              key={row.key}
              data-testid={`budget-modal-row-${row.categoryId ?? row.key}`}
              className="flex items-center gap-2"
            >
              <div className="flex min-w-0 flex-1 flex-col gap-1">
                <label htmlFor={`budget-modal-row-name-${row.key}`} className="sr-only">
                  {BUDGET_COPY.categoryName}
                </label>
                <input
                  id={`budget-modal-row-name-${row.key}`}
                  type="text"
                  value={row.name}
                  onChange={(event) => renameRow(row.key, event.target.value)}
                  // min-h-11/sm:min-h-0: the row's own ✕ button (below) is
                  // already h-11 on a phone, so this doesn't change the
                  // row's height -- it aligns the name and cap fields with a
                  // target that was already 44px instead of floating short
                  // inside it.
                  //
                  // The wrapper's own min-w-0: a flex item's default
                  // min-width is `auto`, which resolves to its content's
                  // min-content width in a row flex container -- for a
                  // column wrapping a text input, that is the input's own
                  // unshrinkable intrinsic width (TransactionFilters.tsx's
                  // FIELD_CLASS documents the identical trap). Without it,
                  // this row's four cells (name, the w-28 cap field, Archive
                  // and the w-11 ✕ button) never lose enough combined width
                  // to fit 375px, and the row scrolled inside the dialog's
                  // own box -- invisible to a check of
                  // `document.documentElement`, since a native <dialog>
                  // paints in the top layer, outside normal document flow.
                  className="min-h-11 rounded-lg border border-hairline bg-card px-3 py-2 text-[13px] sm:min-h-0"
                />
                {(row.archived || row.queuedArchive) && (
                  <span className="text-[11px] text-muted">
                    {row.action === "restore" ? BUDGET_COPY.willRestore : BUDGET_COPY.archivedMarker}
                  </span>
                )}
              </div>
              <div className="flex w-28 flex-col gap-1">
                <label htmlFor={`budget-modal-row-cap-${row.key}`} className="sr-only">
                  {BUDGET_COPY.cap}
                </label>
                <input
                  id={`budget-modal-row-cap-${row.key}`}
                  type="text"
                  inputMode="decimal"
                  value={row.capInput}
                  onChange={(event) => capRow(row.key, event.target.value)}
                  className="min-h-11 rounded-lg border border-hairline bg-card px-3 py-2 text-[13px] sm:min-h-0"
                />
              </div>
              <button
                type="button"
                onClick={() => toggleArchiveRow(row.key)}
                className="min-h-11 text-[11.5px] font-semibold text-label sm:min-h-0"
              >
                {row.queuedArchive ? "Unarchive" : BUDGET_COPY.archiveRow}
              </button>
              <button
                type="button"
                aria-label={BUDGET_COPY.removeRow}
                onClick={() => removeRow(row.key)}
                // 44px floor on phones, restoring at `sm`: same reasoning as
                // Modal.tsx's close button -- this modal isn't tied to the
                // shell's `lg` nav switch.
                className="grid h-11 w-11 flex-none place-items-center rounded-lg bg-canvas text-[12px] text-label sm:h-7 sm:w-7"
              >
                <CloseIcon />
              </button>
            </div>
          ))}
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="budget-modal-add-select" className="text-xs font-semibold text-label">
            {BUDGET_COPY.addACategory}
          </label>
          <select
            id="budget-modal-add-select"
            value={addSelectValue}
            onChange={(event) => handleAddSelectChange(event.target.value)}
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
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
                className="min-h-11 flex-1 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
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
          {addCategoryError !== null && (
            <p role="alert" className="text-xs leading-snug text-danger" data-testid="budget-modal-add-category-error">
              {addCategoryError}
            </p>
          )}
        </div>

        {saveError !== null && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {saveError}
          </p>
        )}

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label sm:min-h-0"
          >
            {BUDGET_COPY.cancel}
          </button>
          <button
            type="button"
            disabled={isSaving}
            onClick={handleSave}
            className="min-h-11 flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          >
            {BUDGET_COPY.saveBudget}
          </button>
        </div>
      </div>
    </Modal>
  );
}
