// A section name, five suggestion chips, and one button that creates the
// section and takes you straight into Propose on it.
//
// Creating a section is immediate and unsigned (decision 8): a heading is not a
// promise, so nothing here proposes anything. The agreement that follows is an
// ordinary proposal, which is why this hands the page a seed rather than
// writing one itself.
import { useId, useState } from "react";
import { Modal } from "../../components/Modal";
import { ApiError } from "../../api/client";
import { AGREEMENT_COPY } from "./agreementCopy";
import { handleWriteError, useAgreements } from "./useAgreements";
import type { AgreementProposeSeed } from "./ProposeAgreementModal";

export function NewSectionModal({
  onCreated,
  onClose,
}: {
  // Handed the seed for the section just created. The page closes this modal
  // and opens Propose with it -- nothing here stacks <dialog>s.
  onCreated: (seed: AgreementProposeSeed) => void;
  onClose: () => void;
}) {
  const { createSection, isCreatingSection, reload } = useAgreements();
  const nameFieldId = useId();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);

  async function handleCreate() {
    setError(null);
    try {
      // createSection is the one write that returns more than the document,
      // precisely so this line has an id to seed Propose with.
      const { section } = await createSection(name.trim());
      onCreated({ mode: "add", sectionId: section.id });
    } catch (err) {
      // The one refusal this field can name (decision 19). It keeps the modal
      // open with the typed name intact -- a generic red box four clicks in is
      // the dead end criterion 13 exists to catch -- and deliberately does NOT
      // reload: nothing about the document changed, and a refetch here would
      // make a name clash look like the screen resetting under the person.
      if (err instanceof ApiError && err.code === "AGREEMENT_SECTION_NAME_TAKEN") {
        setError(AGREEMENT_COPY.sectionNameTaken);
        return;
      }
      setError(handleWriteError(err, reload, AGREEMENT_COPY.newSectionFallbackError));
    }
  }

  return (
    <Modal open onClose={onClose} title={AGREEMENT_COPY.newSectionTitle}>
      <p className="-mt-2 mb-4 text-xs text-muted">{AGREEMENT_COPY.newSectionSubtitle}</p>

      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          // Same guard as the propose modal's: a single-input form implicitly
          // submits on Enter, and both footer buttons are type="button".
          event.preventDefault();
        }}
      >
        <div className="flex flex-col gap-1.5">
          <label htmlFor={nameFieldId} className="text-xs font-semibold text-label">
            {AGREEMENT_COPY.sectionNameLabel}
          </label>
          <input
            id={nameFieldId}
            type="text"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={AGREEMENT_COPY.sectionNamePlaceholder}
            // MaxAgreementSectionNameLen, as a courtesy; the server counts runes.
            maxLength={60}
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
          />
          {/* Directly under the input, never a footer banner: the refusal is
              about this field, and criterion 13 is that the person can see
              which one. */}
          {error !== null && (
            <p
              data-testid="agreement-section-name-error"
              role="alert"
              className="text-xs leading-snug text-danger"
            >
              {error}
            </p>
          )}
        </div>

        <div className="flex flex-col gap-2">
          <span className="text-xs font-semibold text-label">
            {AGREEMENT_COPY.suggestionsLabel}
          </span>
          <div className="flex flex-wrap gap-2">
            {AGREEMENT_COPY.sectionSuggestions.map((suggestion) => (
              // Fills the input; does not submit. A chip that created the
              // section would turn one click into a write nobody got to
              // rename, and the design draws these as suggestions under the
              // field rather than as choices that act.
              <button
                key={suggestion}
                type="button"
                onClick={() => setName(suggestion)}
                className="min-h-11 rounded-full border border-hairline px-3.5 text-[12.5px] text-label sm:min-h-0 sm:py-1.5"
              >
                {suggestion}
              </button>
            ))}
          </div>
        </div>

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label sm:min-h-0"
          >
            {AGREEMENT_COPY.cancel}
          </button>
          <button
            type="button"
            disabled={isCreatingSection || name.trim() === ""}
            onClick={() => void handleCreate()}
            className="min-h-11 flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          >
            {AGREEMENT_COPY.newSectionCreate}
          </button>
        </div>
      </form>
    </Modal>
  );
}
