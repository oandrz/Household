// The three-mode editor behind every entry point that proposes a change: the
// header's "Propose a change", the empty state's "Add your first agreement",
// New section's "Create & add first agreement" and Version history's Restore.
// All four hand it one seed -- { mode, sectionId?, targetAgreementId?, body? },
// every field a pre-fill -- so there is one editor here and not four.
//
// It is the only component in this feature that holds a draft, which is why
// the `hadConflict` latch lives here and nowhere else, and why AgreementsPage
// renders it only while it is open rather than keeping it mounted behind
// `open={false}`: unmounting is the only way back to a writable send button.
import { useId, useState } from "react";
import { Modal } from "../../components/Modal";
import { ApiError } from "../../api/client";
import { AGREEMENT_COPY } from "./agreementCopy";
import { handleWriteError, useAgreements } from "./useAgreements";
import type { AgreementKind, AgreementSection } from "./agreementSchemas";

// The pre-fill, not the request: `mode` becomes the request's `kind`, and the
// three optional fields are whatever the caller already knows. AgreementsPage
// holds one of these in state; Tasks 14 and 15 hand it one.
export type AgreementProposeSeed = {
  // AgreementKind, not a second literal union: two unions for one server enum
  // is how the two drift apart.
  mode: AgreementKind;
  sectionId?: string;
  targetAgreementId?: string;
  body?: string;
};

const MODES: { value: AgreementProposeSeed["mode"]; label: string }[] = [
  { value: "add", label: AGREEMENT_COPY.modeAdd },
  { value: "edit", label: AGREEMENT_COPY.modeEdit },
  { value: "remove", label: AGREEMENT_COPY.modeRemove },
];

const LABEL_CLASS = "text-xs font-semibold text-label";
const TEXTAREA_CLASS =
  "min-h-20 rounded-[10px] border border-hairline bg-card px-3.5 py-3 text-[13px] leading-relaxed";
// w-full min-w-0 is load-bearing, not cosmetic: a <select> sizes itself to its
// longest <option>, and an option here carries an agreement's full body (up to
// 500 characters). SignInScreen.tsx:231 records the same defect measured in a
// real browser -- one long currency option held the sign-up card at 428px on a
// 375px phone and the page scrolled sideways.
const SELECT_CLASS =
  "min-h-11 w-full min-w-0 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0";

export function ProposeAgreementModal({
  seed,
  coOwnerNames,
  sections,
  onOpenNewSection,
  onClose,
}: {
  seed: AgreementProposeSeed;
  // Whose names the subtitle and the remove warning print. Names, never
  // permission: Agree and Withdraw read the server's own canAgree/canWithdraw.
  coOwnerNames: string[];
  // Every section, empty ones included: the document hides an empty section
  // (decision 8) and this picker still offers it, which is what makes Restore
  // into a section that has since emptied work at all.
  sections: AgreementSection[];
  onOpenNewSection: () => void;
  onClose: () => void;
}) {
  const { propose, isProposing, reload } = useAgreements();

  const modeLegendId = useId();
  const sectionSelectId = useId();
  const targetSelectId = useId();
  const bodyFieldId = useId();
  const noteFieldId = useId();

  // Every live agreement, in document order, flattened once: the target select
  // offers these and `bodyOf` reads them.
  const targets = sections.flatMap((section) => section.agreements);

  // One agreement's current wording, by id. The edit pre-fill and the
  // `previousBody` this modal sends both read it, and they must read the same
  // thing -- the server compares `previousBody` against the live row and
  // refuses a mismatch (decision 13), so wording the proposer never saw must
  // never be what gets compared.
  function bodyOf(agreementId: string): string {
    return targets.find((agreement) => agreement.id === agreementId)?.body ?? "";
  }

  // A select whose state is "" while it displays its first option is the dead
  // end docs/LEARNING.md records for BillsPage: what you see is not what gets
  // sent. So edit and remove start on a real agreement, never on nothing.
  const initialTargetId =
    seed.mode === "add" ? "" : (seed.targetAgreementId ?? targets[0]?.id ?? "");

  const [mode, setMode] = useState<AgreementProposeSeed["mode"]>(seed.mode);
  const [sectionId, setSectionId] = useState(seed.sectionId ?? sections[0]?.id ?? "");
  const [targetId, setTargetId] = useState(initialTargetId);
  // Seeded from the seed's own body (Restore's pre-fill) or, on an edit, from
  // the target's current wording -- "New wording, pre-filled with the current
  // body", so nobody retypes what is already there.
  const [body, setBody] = useState(
    seed.body ?? (seed.mode === "edit" ? bodyOf(initialTargetId) : ""),
  );
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  // One-way, component-local, set from err.code -- never useAgreements' own
  // state, which clears on the next background refetch and would re-enable
  // Send over wording that has since moved. RetroModal.tsx:101 and
  // VisionModal.tsx:470 are the same latch for the same defect: once true it
  // never goes false again for this mount's life, and the way back is closing
  // the modal, which unmounts this component and its draft together.
  const [hadConflict, setHadConflict] = useState(false);

  // Clears the OTHER mode's inputs. A hidden stale value that still submits is
  // worse than an empty one (spec, The modals).
  function chooseMode(next: AgreementProposeSeed["mode"]) {
    const nextTargetId = next === "add" ? "" : (targets[0]?.id ?? "");
    setMode(next);
    setTargetId(nextTargetId);
    setBody(next === "edit" ? bodyOf(nextTargetId) : "");
  }

  function chooseTarget(nextTargetId: string) {
    setTargetId(nextTargetId);
    // Only an edit pre-fills; a remove sends no body at all.
    if (mode === "edit") setBody(bodyOf(nextTargetId));
  }

  // An edit or a remove with no target selects nothing to change, and an add
  // or an edit with no wording proposes nothing. Both are shapes the server
  // refuses with ErrAgreementProposalShapeInvalid, so Send stays off rather
  // than sending a request that can only 422. The empty select above it is
  // what says why: a household with no agreements at all cannot edit one.
  const canSend =
    mode === "add"
      ? body.trim() !== ""
      : targetId !== "" && (mode === "remove" || body.trim() !== "");

  async function handleSend() {
    setError(null);
    try {
      await propose({
        kind: mode,
        // The handler blanks the section for anything but an add anyway (the
        // section is the target's), and Validate refuses a caller-supplied one
        // as its fail-closed backstop. Sending "" keeps the two sides agreeing.
        sectionId: mode === "add" ? sectionId : "",
        targetAgreementId: mode === "add" ? "" : targetId,
        body: mode === "remove" ? "" : body.trim(),
        previousBody: mode === "add" ? "" : bodyOf(targetId),
        note: note.trim(),
      });
      onClose();
    } catch (err) {
      // handleWriteError first, always: it is behaviour, not copy -- it is
      // what calls reload(), and the document has moved under this draft
      // whichever refusal came back. Its return value is the message for the
      // failures it does not name itself.
      const message = handleWriteError(err, reload, AGREEMENT_COPY.proposeFallbackError);
      if (err instanceof ApiError && err.code === "AGREEMENT_CHANGED") {
        // The conflict has its own banner, which says more than one line can:
        // stay open, keep what is typed, start again from the current wording.
        // Setting `error` too would print the same refusal twice.
        setHadConflict(true);
        return;
      }
      setError(message);
    }
  }

  return (
    <Modal open onClose={onClose} title={AGREEMENT_COPY.proposeTitle}>
      <p className="-mt-2 mb-4 text-xs text-muted">
        {AGREEMENT_COPY.proposeSubtitle(coOwnerNames)}
      </p>

      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          // The form submits nothing. RetroModal.tsx:355-365 is the same guard
          // for the same reason: a `type="submit"` once let Enter anywhere in
          // a form finish a retro. Enter here would send a proposal both
          // owners then have to live with.
          event.preventDefault();
        }}
      >
        <div>
          <h3 id={modeLegendId} className={`mb-2.5 ${LABEL_CLASS}`}>
            {AGREEMENT_COPY.changeTypeLabel}
          </h3>
          {/* Three real radios sharing one `name`: one tab stop, arrow keys
              between options, a visible focus ring on a real on-screen
              element -- all free from the platform. NOT sr-only inputs behind
              a styled stand-in, which is the shape that shipped
              keyboard-invisible focus in TransactionsPage's Kind filter
              (docs/LEARNING.md pattern 3) with every unit test green, because
              fireEvent.click never presses a key. RetroModal.tsx:400-413 is
              the corrected shape this copies. */}
          <div role="radiogroup" aria-labelledby={modeLegendId} className="flex gap-1">
            {MODES.map((option) => (
              <label
                key={option.value}
                className={`flex min-h-11 flex-1 cursor-pointer items-center justify-center gap-1.5 rounded-[10px] border py-2 text-[12.5px] sm:min-h-0 ${
                  mode === option.value ? "border-accent bg-callout" : "border-hairline"
                }`}
              >
                <input
                  type="radio"
                  name="agreement-mode"
                  checked={mode === option.value}
                  onChange={() => chooseMode(option.value)}
                  className="h-4 w-4 accent-accent"
                />
                {option.label}
              </label>
            ))}
          </div>
        </div>

        {mode === "add" && (
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between gap-3">
              <label htmlFor={sectionSelectId} className={LABEL_CLASS}>
                {AGREEMENT_COPY.addSectionLabel}
              </label>
              {/* Opens New section from inside this one. The page closes this
                  modal before opening that one -- nothing here stacks
                  <dialog>s -- and hands the new section straight back as a
                  fresh seed. */}
              <button
                type="button"
                onClick={onOpenNewSection}
                className="min-h-11 text-xs font-semibold text-accent sm:min-h-0"
              >
                {AGREEMENT_COPY.newSectionLink}
              </button>
            </div>
            <select
              id={sectionSelectId}
              value={sectionId}
              onChange={(event) => setSectionId(event.target.value)}
              className={SELECT_CLASS}
            >
              {sections.map((section) => (
                <option key={section.id} value={section.id}>
                  {section.name}
                </option>
              ))}
            </select>
          </div>
        )}

        {mode !== "add" && (
          <div className="flex flex-col gap-1.5">
            <label htmlFor={targetSelectId} className={LABEL_CLASS}>
              {mode === "edit"
                ? AGREEMENT_COPY.editTargetLabel
                : AGREEMENT_COPY.removeTargetLabel}
            </label>
            <select
              id={targetSelectId}
              value={targetId}
              onChange={(event) => chooseTarget(event.target.value)}
              className={SELECT_CLASS}
            >
              {targets.map((agreement) => (
                <option key={agreement.id} value={agreement.id}>
                  {AGREEMENT_COPY.targetOption(agreement.number, agreement.body)}
                </option>
              ))}
            </select>
          </div>
        )}

        {mode !== "remove" && (
          <div className="flex flex-col gap-1.5">
            <label htmlFor={bodyFieldId} className={LABEL_CLASS}>
              {mode === "add" ? AGREEMENT_COPY.addBodyLabel : AGREEMENT_COPY.editBodyLabel}
            </label>
            <textarea
              id={bodyFieldId}
              value={body}
              onChange={(event) => setBody(event.target.value)}
              placeholder={mode === "add" ? AGREEMENT_COPY.addBodyPlaceholder : undefined}
              rows={3}
              // Mirrors MaxAgreementBodyLen as a courtesy; the rune-counting
              // server is the authority. maxLength counts UTF-16 code units,
              // so an emoji costs two here and one there -- the browser is the
              // stricter of the two on that input, which is the safe direction
              // for a cap that exists only to stop someone typing 4,000
              // characters into a box the server will refuse.
              maxLength={500}
              className={TEXTAREA_CLASS}
            />
          </div>
        )}

        {mode === "remove" && (
          // The existing danger pair, as AdminMailPage.tsx:63 uses it and as
          // RetroDetail.tsx:146-152 paints the design's own warm card.
          <p
            data-testid="agreement-remove-warning"
            className="rounded-[10px] border border-danger-border bg-danger-soft p-3.5 text-[12.5px] leading-relaxed text-danger"
          >
            {AGREEMENT_COPY.removeWarning(coOwnerNames)}
          </p>
        )}

        <div className="flex flex-col gap-1.5">
          <label htmlFor={noteFieldId} className={LABEL_CLASS}>
            {AGREEMENT_COPY.proposeNoteLabel}
          </label>
          <textarea
            id={noteFieldId}
            value={note}
            onChange={(event) => setNote(event.target.value)}
            placeholder={AGREEMENT_COPY.proposeNotePlaceholder(coOwnerNames)}
            rows={2}
            maxLength={500}
            className={TEXTAREA_CLASS}
          />
        </div>

        {/* Only Restore seeds an add with a body already in it (decision 18),
            so this sentence appears exactly where restoring is what is
            happening -- and says the thing the button cannot: this is a
            proposal like any other, not a one-click undo. */}
        {seed.mode === "add" && seed.body ? (
          <p className="text-xs leading-relaxed text-muted">{AGREEMENT_COPY.restoreNote}</p>
        ) : null}

        {hadConflict && (
          <p
            data-testid="agreement-propose-conflict"
            role="alert"
            className="rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] leading-relaxed text-danger"
          >
            {AGREEMENT_COPY.proposeConflict}
          </p>
        )}

        {error !== null && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {error}
          </p>
        )}

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label sm:min-h-0"
          >
            {AGREEMENT_COPY.cancel}
          </button>
          {/* type="button", never "submit": see the form's onSubmit above. */}
          <button
            type="button"
            disabled={hadConflict || isProposing || !canSend}
            onClick={() => void handleSend()}
            className="min-h-11 flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          >
            {AGREEMENT_COPY.proposeSend}
          </button>
        </div>
      </form>
    </Modal>
  );
}
