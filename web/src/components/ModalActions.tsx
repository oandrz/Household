// The footer a form modal ends with: a secondary button on the left and the
// primary action beside it at twice the width.
//
// Extracted because the same pair of buttons, class for class, had been
// copied into twelve modals: Invite member, New space, New section, Propose
// agreement, Retro, Vision, Budget, Transaction, Goal, Account, Bill and
// Mark paid.
//
// The secondary button is Cancel in every one of them but RetroModal, where it
// is "Save draft". That is why it takes its own label, handler and disabled
// flag rather than being hard-wired to close the modal.
//
// Whether the primary button submits the form is a REQUIRED choice, never a
// default. A type="submit" button is the one the browser clicks when someone
// presses Enter in any text field of that form. RetroModal, VisionModal,
// BudgetModal, NewSectionModal and ProposeAgreementModal must not have one:
// Enter once finished a retro -- which cannot be undone -- and threw away the
// action the household was still typing (useRetroDraft.ts's `finish`
// comment). A default of "submit" would bring that back for the next modal
// that forgot to say.
type PrimaryAction =
  // The enclosing <form>'s own onSubmit does the work, and Enter in a text
  // field submits it.
  | { primaryType: "submit" }
  // A plain button: only a click reaches onPrimary, never Enter.
  | { primaryType: "button"; onPrimary: () => void };

type ModalActionsProps = {
  secondaryLabel: string;
  onSecondary: () => void;
  secondaryDisabled?: boolean;
  primaryLabel: string;
  primaryDisabled?: boolean;
} & PrimaryAction;

export function ModalActions(props: ModalActionsProps) {
  return (
    <div className="mt-1 flex gap-2.5">
      <button
        type="button"
        disabled={props.secondaryDisabled}
        onClick={props.onSecondary}
        className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
      >
        {props.secondaryLabel}
      </button>
      <button
        type={props.primaryType}
        disabled={props.primaryDisabled}
        onClick={props.primaryType === "button" ? props.onPrimary : undefined}
        className="min-h-11 flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
      >
        {props.primaryLabel}
      </button>
    </div>
  );
}
