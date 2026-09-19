// One capped category in the Edit-budget modal: its name, its cap, and the
// Archive and ✕ controls. Pulled out of BudgetModal.tsx -- the same
// one-row-one-component shape BillRow.tsx and GoalCard.tsx use -- with its
// markup and comments moved here unchanged. It holds no state of its own:
// every change goes back up through the callbacks to useBudgetRows, which
// owns the rows.
import { CloseIcon } from "../../components/icons";
import { BUDGET_COPY } from "./budgetCopy";
import type { BudgetRow } from "./useBudgetRows";

export function BudgetCategoryRow({
  row,
  onRename,
  onCapChange,
  onToggleArchive,
  onRemove,
}: {
  row: BudgetRow;
  onRename: (name: string) => void;
  onCapChange: (capInput: string) => void;
  onToggleArchive: () => void;
  onRemove: () => void;
}) {
  return (
    <div data-testid={`budget-modal-row-${row.categoryId ?? row.key}`} className="flex items-center gap-2">
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <label htmlFor={`budget-modal-row-name-${row.key}`} className="sr-only">
          {BUDGET_COPY.categoryName}
        </label>
        <input
          id={`budget-modal-row-name-${row.key}`}
          type="text"
          value={row.name}
          onChange={(event) => onRename(event.target.value)}
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
          onChange={(event) => onCapChange(event.target.value)}
          className="min-h-11 rounded-lg border border-hairline bg-card px-3 py-2 text-[13px] sm:min-h-0"
        />
      </div>
      <button
        type="button"
        onClick={onToggleArchive}
        className="min-h-11 text-[11.5px] font-semibold text-label sm:min-h-0"
      >
        {row.queuedArchive ? "Unarchive" : BUDGET_COPY.archiveRow}
      </button>
      <button
        type="button"
        aria-label={BUDGET_COPY.removeRow}
        onClick={onRemove}
        // 44px floor on phones, restoring at `sm`: same reasoning as
        // Modal.tsx's close button -- this modal isn't tied to the
        // shell's `lg` nav switch.
        className="grid h-11 w-11 flex-none place-items-center rounded-lg bg-canvas text-[12px] text-label sm:h-7 sm:w-7"
      >
        <CloseIcon />
      </button>
    </div>
  );
}
