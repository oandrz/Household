// The New/Edit goal modal (design/Household Dashboard.dc.html's "NEW GOAL"
// panel). Follows AccountModal.tsx's shape most closely: a plain <form>, a
// currency select, an isEditing branch that hides/disables the fields that
// only make sense once -- but calls `useGoals()` itself rather than taking
// a mutation as a prop, the same asymmetry BudgetModal.tsx has against
// TransactionModal.tsx (its own header comment explains why: this component
// is never hand a mutation the way TransactionModal is, because nothing
// upstream of it has already resolved one).
//
// Two fields are create-only, and this is the whole reason the component
// exists rather than being one more form AccountModal-shaped:
//
// - Currency cannot change after creation. Every contribution is
//   denominated in the goal's currency by construction (goal_contributions
//   carries no currency column of its own), so changing it would silently
//   restate a multi-year total. The backend refuses this at the type level
//   (usecase.GoalUpdate has no Currency field at all -- see that type's own
//   comment) and again at the handler (domain.ErrGoalCurrencyImmutable, if a
//   caller puts a "currency" key in a PATCH body anyway). This form's job is
//   to make that refusal visible *before* the request, not just survive it.
// - Starting balance exists only at creation. Spec decision 8: it is
//   written as a `starting_balance` contribution row, not a column on the
//   goal, specifically to avoid the shape of the Critical defect Accounts
//   already shipped once -- a field prefilled from a derived value
//   (AccountModal's Balance, prefilled from the then-current balance) and
//   written back as the opening one, silently moving a household total on
//   every edit (docs/LEARNING.md pattern 1). Once a goal exists, the
//   contributions ledger owns that figure; this form does not offer a
//   second way to move it.
import { type FormEvent, useState } from "react";
import { FieldPair } from "../../components/FieldPair";
import { Modal } from "../../components/Modal";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { ApiError } from "../../api/client";
import { apiErrorMessage } from "../auth/copy";
import type { Currency } from "../auth/schemas";
import { describeAmountError, formatMoney, minorUnitsToInputValue, toMinorUnits } from "./formatMoney";
import { useGoals, type CreateGoalBody, type UpdateGoalBody } from "./useGoals";
import type { Goal } from "./goalSchemas";

// currentMonthValue reads the *local* calendar month via getFullYear/
// getMonth, never toISOString() (which converts to UTC first) -- the same
// function, and the same reason, as AccountModal.tsx's own today(): a
// caller at 7am in Singapore (UTC+8) computing "this month" through UTC
// could still read the previous month. Not imported from AccountModal.tsx
// (a small duplicated function is a smaller risk than coupling two
// features' date handling through one import for four lines), the same
// call TransactionModal.tsx's own today() comment already made.
function currentMonthValue(): string {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  return `${year}-${month}`;
}

// targetMonthLabel mirrors GoalCard.tsx's own private helper of the same
// name ("2026-12" -> "Dec 2026"), duplicated rather than imported because
// GoalCard.tsx does not export it and reaching into a sibling component's
// internals for four lines is the wrong seam. Anchored on day 2, not day 1,
// for the identical reason: a Date built on day 1 reads back as the last
// day of the *previous* month at a negative UTC offset.
function targetMonthLabel(month: string): string {
  const [year, monthNum] = month.split("-").map(Number);
  return new Date(year, monthNum - 1, 2).toLocaleDateString("en-US", {
    month: "short",
    year: "numeric",
  });
}

// monthsLeftInclusive mirrors domain.MonthsLeftInclusive
// (api/internal/domain/goal.go) exactly: whole calendar months from the
// current month to the target, counting both ends (Aug -> Dec is 5; a
// target in the current month is 1), and 0 -- never negative -- once the
// target month has passed. The suggestion below must agree with the
// "Required monthly" figure GoalCard shows once the goal is saved, or the
// same goal would read two different monthly figures depending on which
// screen showed it.
function monthsLeftInclusive(targetMonth: string, todayMonth: string): number {
  const [targetYear, targetMonthNum] = targetMonth.split("-").map(Number);
  const [todayYear, todayMonthNum] = todayMonth.split("-").map(Number);
  const months = (targetYear - todayYear) * 12 + (targetMonthNum - todayMonthNum);
  return months < 0 ? 0 : months + 1;
}

// suggestedMonthlyMinor mirrors domain.RequiredMonthlyMinor: ceiling
// division, and null -- not a fabricated number -- when there is no honest
// figure to divide into (monthsLeft <= 0, a target month already past).
// Rounding up is deliberate there and here: rounding down states a figure
// that does not actually reach the target.
//
// Done in BigInt, not `Math.floor(a / b)` -- JS's `/` is always IEEE-754
// double division, and while every realistic minor-unit figure here is
// comfortably within Number.MAX_SAFE_INTEGER, a division that does not come
// out even (the whole point of a ceiling division) is exactly the case
// `minorUnitsToInputValue`'s own comment calls out as unsafe to divide with
// `/` -- "no floating-point rounding enters" only holds there because that
// division is guaranteed exact. Go's own RequiredMonthlyMinor is int64
// arithmetic with no floating point at all; BigInt is what actually matches
// that, rather than trusting double-precision rounding to agree with it.
function suggestedMonthlyMinor(remainingMinor: number, monthsLeft: number): number | null {
  if (monthsLeft <= 0) return null;
  if (remainingMinor <= 0) return 0;
  const remaining = BigInt(remainingMinor);
  const months = BigInt(monthsLeft);
  return Number((remaining + months - 1n) / months);
}

export function GoalModal({
  mode,
  goal,
  currencies,
  primaryCurrency,
  onClose,
  onSaved,
}: {
  mode: "create" | "edit";
  // Present only when mode === "edit" -- the caller's contract (GoalsPage's
  // own modalGoal state is "new" | Goal | null, never "edit" paired with no
  // goal), the identical shape TransactionModal.tsx's own `initial?`
  // documents rather than enforces with a discriminated-union prop type.
  goal?: Goal;
  currencies: Currency[];
  primaryCurrency: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const isEditing = mode === "edit";
  const { createGoal, updateGoal, restoreGoal } = useGoals({
    // This component only ever calls the mutations below -- it never reads
    // the list itself (every field it needs arrives via props: `goal` in
    // edit mode, `currencies`/`primaryCurrency` always). `enabled: false`
    // is useGoals' own escape hatch for exactly this caller shape (its own
    // header comment names Overview as the precedent), and it means this
    // modal fires no GET of its own -- a write still invalidates the *other*
    // mounted `useGoals()` instance (GoalsPage's), which is the one that
    // actually needs to refetch.
    enabled: false,
  });

  const [name, setName] = useState(goal?.name ?? "");
  const [currency, setCurrency] = useState(goal?.currency ?? primaryCurrency);
  const [targetAmountInput, setTargetAmountInput] = useState(() =>
    goal ? minorUnitsToInputValue(goal.targetMinor, goal.currency) : "",
  );
  const [noTargetDate, setNoTargetDate] = useState(() => (goal ? goal.targetMonth === null : false));
  const [targetMonthInput, setTargetMonthInput] = useState(goal?.targetMonth ?? "");
  const [startingBalanceInput, setStartingBalanceInput] = useState("0");
  const [plannedMonthlyInput, setPlannedMonthlyInput] = useState(() =>
    goal ? minorUnitsToInputValue(goal.plannedMonthlyMinor, goal.currency) : "",
  );

  const [targetError, setTargetError] = useState<string | null>(null);
  const [targetMonthError, setTargetMonthError] = useState<string | null>(null);
  const [startingBalanceError, setStartingBalanceError] = useState<string | null>(null);
  const [plannedMonthlyError, setPlannedMonthlyError] = useState<string | null>(null);
  const [saveError, setSaveError] = useState<string | null>(null);
  // Set only for a 409 GOAL_NAME_TAKEN whose body names an archived goal --
  // Task 8's writeGoalNameConflict, which looks the archived goal up itself
  // so this modal never has to guess which one collided.
  const [restoreGoalId, setRestoreGoalId] = useState<string | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [isRestoring, setIsRestoring] = useState(false);

  const symbol = currencies.find((c) => c.code === currency)?.symbol;

  // The live suggestion (spec's "Suggested monthly (modal)": remaining ÷
  // monthsLeft at the values currently typed, recomputed on every render --
  // computed here, not in an effect, the same "settle before this render
  // commits" pattern TransactionModal.tsx's own Amount-received mirroring
  // uses). null hides the panel outright rather than rendering a garbage or
  // zero figure: while the target amount doesn't parse to a positive
  // number, while no target month is chosen, and once a chosen target month
  // has already passed (monthsLeftInclusive's own 0-not-negative contract).
  const parsedTargetMinor = toMinorUnits(targetAmountInput, currency);
  const suggestion = (() => {
    if (noTargetDate || targetMonthInput === "") return null;
    if (parsedTargetMinor === null || parsedTargetMinor <= 0) return null;
    // Create mode has no contributed figure yet except whatever starting
    // balance is currently typed; edit mode's real progress is the goal's
    // own contributedMinor (the starting-balance field does not exist in
    // edit mode, so there is nothing else it could be).
    const contributedMinor = isEditing ? goal!.contributedMinor : (toMinorUnits(startingBalanceInput, currency) ?? 0);
    const remainingMinor = Math.max(0, parsedTargetMinor - contributedMinor);
    const monthsLeft = monthsLeftInclusive(targetMonthInput, currentMonthValue());
    return suggestedMonthlyMinor(remainingMinor, monthsLeft);
  })();

  function handleNoTargetDateToggle() {
    setNoTargetDate((prev) => !prev);
    setTargetMonthError(null);
  }

  async function handleSave(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setTargetError(null);
    setTargetMonthError(null);
    setStartingBalanceError(null);
    setPlannedMonthlyError(null);
    setSaveError(null);
    setRestoreGoalId(null);

    const targetMinor = toMinorUnits(targetAmountInput, currency);
    if (targetMinor === null) {
      setTargetError(describeAmountError(targetAmountInput, currency, "10000.00"));
      return;
    }
    if (targetMinor <= 0) {
      setTargetError("Enter a target greater than zero.");
      return;
    }

    if (!noTargetDate && targetMonthInput === "") {
      setTargetMonthError('Choose a target month, or turn on "No target date."');
      return;
    }

    let startingBalanceMinor = 0;
    if (!isEditing) {
      const parsedStartingBalance = toMinorUnits(startingBalanceInput, currency);
      if (parsedStartingBalance === null) {
        setStartingBalanceError(describeAmountError(startingBalanceInput, currency, "0.00"));
        return;
      }
      startingBalanceMinor = parsedStartingBalance;
    }

    const plannedMonthlyMinor = toMinorUnits(plannedMonthlyInput, currency);
    if (plannedMonthlyMinor === null) {
      setPlannedMonthlyError(describeAmountError(plannedMonthlyInput, currency, "550.00"));
      return;
    }
    if (plannedMonthlyMinor < 0) {
      setPlannedMonthlyError("Enter a monthly amount of zero or more.");
      return;
    }

    const trimmedName = name.trim();

    setIsSaving(true);
    try {
      if (isEditing) {
        // Every field but the date is safe to resend unconditionally: none
        // of name/targetMinor/plannedMonthlyMinor is a derived figure this
        // form could restate wrongly the way AccountModal's Balance once
        // was (docs/LEARNING.md pattern 1) -- they are exactly what is
        // showing in the field, prefilled from the same goal this PATCH
        // targets. The date is the one field with two distinct "leave
        // alone" and "clear" states, so it alone needs the explicit
        // clearTargetMonth flag -- the same convention
        // clearReceivedAmount uses on transactions (useGoals.ts's own
        // UpdateGoalBody comment).
        const body: UpdateGoalBody = {
          name: trimmedName,
          targetMinor,
          plannedMonthlyMinor,
        };
        if (noTargetDate) {
          body.clearTargetMonth = true;
        } else {
          body.targetMonth = targetMonthInput;
        }
        // mode === "edit" guarantees the caller passed `goal` (this
        // component's own contract, documented on the prop above).
        await updateGoal(goal!.id, body);
      } else {
        const body: CreateGoalBody = {
          name: trimmedName,
          targetMinor,
          currency,
          targetMonth: noTargetDate ? null : targetMonthInput,
          plannedMonthlyMinor,
          startingBalanceMinor,
        };
        await createGoal(body);
      }
      onSaved();
      onClose();
    } catch (err) {
      if (err instanceof ApiError && err.code === "GOAL_NAME_TAKEN") {
        const archivedGoalId =
          typeof err.details.archivedGoalId === "string" ? err.details.archivedGoalId : null;
        if (archivedGoalId) {
          // writeGoalNameConflict's own message already names the archived
          // goal and explains Restore -- nothing to build client-side here.
          setSaveError(err.message);
          setRestoreGoalId(archivedGoalId);
        } else {
          // The generic GOAL_NAME_TAKEN message never carries the attempted
          // name (MapDomainError's own default case) -- the same gap
          // BudgetModal.tsx's categoryNameTaken fills for categories.
          setSaveError(`"${trimmedName}" is already the name of a goal in this household.`);
        }
      } else {
        setSaveError(apiErrorMessage(err, "Something went wrong. Please try again."));
      }
    } finally {
      setIsSaving(false);
    }
  }

  async function handleRestore() {
    if (!restoreGoalId) return;
    setIsRestoring(true);
    try {
      // Restoring brings the archived goal itself back -- it is the one
      // that already holds this name, so there is nothing left to create.
      // A retried create would just 409 again against the goal this call
      // just un-archived.
      await restoreGoal(restoreGoalId);
      onSaved();
      onClose();
    } catch (err) {
      setSaveError(apiErrorMessage(err, "Something went wrong. Please try again."));
    } finally {
      setIsRestoring(false);
    }
  }

  return (
    <Modal open onClose={onClose} title="Goal details">
      <form className="flex flex-col gap-4" onSubmit={handleSave}>
        <div className="flex flex-col gap-1.5">
          <label htmlFor="goal-modal-name" className="text-xs font-semibold text-label">
            Goal name
          </label>
          <input
            id="goal-modal-name"
            type="text"
            required
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="e.g. Japan 2027, new sofa, rainy-day fund"
            // min-h-11/sm:min-h-0 on every field in this modal:
            // TransactionFilters.tsx's own SELECT_CLASS comment has the
            // measured reason py-2.5 alone falls short of the 44px floor
            // on a phone.
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
          />
        </div>

        <FieldPair>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="goal-modal-target-amount" className="text-xs font-semibold text-label">
              Target amount
            </label>
            <input
              id="goal-modal-target-amount"
              type="text"
              inputMode="decimal"
              required
              value={targetAmountInput}
              onChange={(event) => setTargetAmountInput(event.target.value)}
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
            />
          </div>

          <div className="flex flex-col gap-1.5">
            <label htmlFor="goal-modal-currency" className="text-xs font-semibold text-label">
              Currency
            </label>
            <select
              id="goal-modal-currency"
              value={currency}
              disabled={isEditing}
              onChange={(event) => setCurrency(event.target.value)}
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
            >
              {!currencies.some((c) => c.code === currency) && <option value={currency}>{currency}</option>}
              {currencies.map((c) => (
                <option key={c.code} value={c.code}>
                  {c.code}
                </option>
              ))}
            </select>
            {/* The reason lives next to the disabled control, not only in a
                tooltip nobody reading this modal would see -- every
                contribution is denominated in this currency by construction
                (goal_contributions carries no currency column), so changing
                it after creation would silently restate a multi-year total. */}
            {isEditing && (
              <p className="text-[11.5px] leading-snug text-muted">
                Currency can&apos;t change after a goal is created — every contribution already counts in it.
              </p>
            )}
          </div>
        </FieldPair>

        {targetError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {targetError}
          </p>
        )}

        <div className="flex flex-col gap-1.5">
          <div className="flex items-center justify-between gap-3">
            <label htmlFor="goal-modal-target-month" className="text-xs font-semibold text-label">
              Target month
            </label>
            <div className="flex items-center gap-1.5 text-[11px] text-muted">
              <ToggleSwitch checked={noTargetDate} onChange={handleNoTargetDateToggle} label="No target date" />
              No target date
            </div>
          </div>
          <input
            id="goal-modal-target-month"
            type="month"
            disabled={noTargetDate}
            value={targetMonthInput}
            onChange={(event) => setTargetMonthInput(event.target.value)}
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          />
          {targetMonthError && (
            <p role="alert" className="text-xs leading-snug text-danger">
              {targetMonthError}
            </p>
          )}
        </div>

        {!isEditing && (
          <div className="flex flex-col gap-1.5">
            <label htmlFor="goal-modal-starting-balance" className="text-xs font-semibold text-label">
              Starting balance
            </label>
            <input
              id="goal-modal-starting-balance"
              type="text"
              inputMode="decimal"
              required
              value={startingBalanceInput}
              onChange={(event) => setStartingBalanceInput(event.target.value)}
              className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
            />
            {startingBalanceError && (
              <p role="alert" className="text-xs leading-snug text-danger">
                {startingBalanceError}
              </p>
            )}
          </div>
        )}

        {/* The suggestion only -- never the editable figure below it, which
            the household controls on its own (spec's own "a suggestion
            only -- the household may type anything, including 0"). Absent
            outright, not blank, while the target or the date doesn't yet
            say enough to compute one. */}
        {suggestion !== null && (
          <div
            data-testid="goal-modal-suggestion"
            className="rounded-[10px] border border-hairline bg-callout px-4 py-3"
          >
            <div className="text-[13px] font-semibold text-accent">Planned each month</div>
            <div className="mt-0.5 text-[11.5px] text-muted">
              To hit {formatMoney(parsedTargetMinor!, currency, symbol)} by{" "}
              {targetMonthLabel(targetMonthInput)}, save ~{formatMoney(suggestion, currency, symbol)}/mo
            </div>
          </div>
        )}

        <div className="flex flex-col gap-1.5">
          <label htmlFor="goal-modal-planned-monthly" className="text-xs font-semibold text-label">
            Planned each month
          </label>
          <input
            id="goal-modal-planned-monthly"
            type="text"
            inputMode="decimal"
            required
            value={plannedMonthlyInput}
            onChange={(event) => setPlannedMonthlyInput(event.target.value)}
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
          />
          {plannedMonthlyError && (
            <p role="alert" className="text-xs leading-snug text-danger">
              {plannedMonthlyError}
            </p>
          )}
        </div>

        {saveError !== null && (
          <div className="flex flex-col gap-2">
            <p role="alert" className="text-xs leading-snug text-danger">
              {saveError}
            </p>
            {restoreGoalId && (
              <button
                type="button"
                disabled={isRestoring}
                onClick={handleRestore}
                // min-h-11/sm:min-h-0: py-2.5 alone measured short of the
                // 44px floor at this text size -- TransactionFilters.tsx's
                // own SELECT_CLASS comment has the measured numbers.
                className="min-h-11 self-start rounded-lg border border-hairline px-3 py-2.5 text-[12.5px] font-semibold text-accent disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0 sm:py-1.5"
              >
                Restore
              </button>
            )}
          </div>
        )}

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label sm:min-h-0"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={isSaving}
            className="min-h-11 flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          >
            {isEditing ? "Save" : "Create goal"}
          </button>
        </div>
      </form>
    </Modal>
  );
}
