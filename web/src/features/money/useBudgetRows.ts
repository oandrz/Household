// The Edit-budget modal's rows -- one per capped category -- and everything
// that adds, removes, renames, re-caps or archives one, plus the "+ Add a
// category" control's own state. Pulled out of BudgetModal.tsx so the modal
// renders and saves; the state, handlers and comments below moved here
// unchanged.
//
// Call it only from BudgetModalForm, never from the outer BudgetModal: the
// rows' `useState(() => buildRows(...))` initialiser reads `initial` and
// `allCategories` exactly once, so it must first run after both have loaded
// (BudgetModalForm's own comment in BudgetModal.tsx explains the split).
import { useRef, useState } from "react";
import { BUDGET_COPY } from "./budgetCopy";
import { fiftyThirtyTwentyTemplate, type TemplatePrefill } from "./budgetTemplates";
import { minorUnitsToInputValue, toMinorUnits } from "./formatMoney";

// The household's plain category list (BudgetPage.tsx's `useCategories()`
// data) -- active only, by construction of that endpoint's default
// `includeArchived=false`. `archived` is typed optional here rather than
// omitted: `kind` is required by both `Category` shapes this modal is
// ever handed (transactionSchemas.ts's and budgetSchemas.ts's), but a
// defensive `!c.archived` check on the add-category dropdown costs nothing
// and guards the shape this prop is documented, not enforced, to hold.
export type CategoryOption = {
  id: string;
  name: string;
  kind: "expense" | "income";
  archived?: boolean;
};

type RowFields = {
  // Stable across re-renders regardless of `action`: a real category's own
  // id for an existing/restored/dropdown-picked row, or a locally-generated
  // temp id for a row still waiting on its create call.
  key: string;
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
};

// One row per capped category in the modal. A union on `action`, so the type
// itself says which rows have a real category id: only a row still waiting on
// its create call has none. The save loop in BudgetModal.tsx relies on that --
// it never has to assert that an id exists.
export type BudgetRow =
  | (RowFields & { action: "create"; categoryId: null })
  | (RowFields & { action: "restore" | null; categoryId: string });

function buildRows(
  lines: TemplatePrefill["lines"],
  categories: CategoryOption[],
  currency: string,
): BudgetRow[] {
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

export function useBudgetRows({
  initial,
  categories,
  allCategories,
  awaitingIncome,
  currency,
}: {
  initial: TemplatePrefill;
  // Active only -- what the add-dropdown offers.
  categories: CategoryOption[];
  // The include-archived roster -- what names a row and spots an archived name.
  allCategories: CategoryOption[];
  awaitingIncome: boolean;
  currency: string;
}) {
  const [rows, setRows] = useState<BudgetRow[]>(() => buildRows(initial.lines, allCategories, currency));
  const [missing, setMissing] = useState<string[]>(initial.missing);
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

  // Only the 50/30/20 template's waiting-for-income state drives caps off
  // income -- every other opening (blank, family-of-four, an existing
  // budget) treats income as a plain editable field with no effect on the
  // rows below it. Recomputes on every keystroke, replacing whatever the
  // template last computed: a household that hand-tweaks a row and then
  // keeps typing into income loses that tweak, an accepted simplification
  // for a template's one-shot prefill role (this is never reachable for
  // the general edit flow, which never sets `awaitingIncome`).
  function prefillFromIncome(incomeInput: string) {
    if (!awaitingIncome) return;
    const minor = incomeInput.trim() === "" ? null : toMinorUnits(incomeInput, currency);
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
    // is attached to (gated on addSelectValue === "__new__" in the modal) --
    // clear it here too so a stale refusal from a previous attempt can't
    // flash back in if the household reopens "New category…" later.
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

  const missingToShow = missing.filter((name) => !rows.some((row) => row.name === name));
  const availableToAdd = categories.filter(
    (c) => c.kind === "expense" && !c.archived && !rows.some((row) => row.categoryId === c.id),
  );

  return {
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
  };
}
