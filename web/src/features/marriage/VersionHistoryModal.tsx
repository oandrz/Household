// Every accepted change, newest first, with Restore on the removals.
//
// It renders `data.history` off the document the page already holds and
// FETCHES NOTHING OF ITS OWN. History travels on the document because it is
// composed from rows the read already walked (decision 9), so a second
// endpoint would refetch what the first threw away and go stale after every
// Agree -- and opening this modal can never show a version the page below it
// disagrees with, because there is only one version to disagree about.
import { useState } from "react";
import { Modal } from "../../components/Modal";
import { AGREEMENT_COPY, historyDateLabel } from "./agreementCopy";
import { useAgreements } from "./useAgreements";
import type { AgreementHistoryEntry } from "./agreementSchemas";
import type { AgreementProposeSeed } from "./ProposeAgreementModal";

// How many rows render before the rest collapse (spec, "…its disclosure").
const NEWEST = 4;

export function VersionHistoryModal({
  onRestore,
  onClose,
}: {
  // Handed a seed for an ordinary add proposal (decision 18). The page closes
  // this modal and opens Propose with it: nothing here stacks <dialog>s, and
  // Task 13's latch wants a fresh mount anyway.
  onRestore: (seed: AgreementProposeSeed) => void;
  onClose: () => void;
}) {
  const { data, isLoading } = useAgreements();
  const [expanded, setExpanded] = useState(false);

  const history = data?.history ?? [];
  const shown = expanded ? history : history.slice(0, NEWEST);
  const collapsed = history.slice(NEWEST);
  // Both bounds off the entries that actually collapsed, the way retroCopy.ts's
  // showOlderYear takes both from data. A hardcoded 1 is wrong by
  // construction: there is no v1 row, because v1 is the document before
  // anything was agreed (decision 10).
  const lowest = collapsed.length > 0 ? Math.min(...collapsed.map((e) => e.version)) : 0;
  const highest = collapsed.length > 0 ? Math.max(...collapsed.map((e) => e.version)) : 0;

  // By VALUE, never by array position: nothing in the schema enforces the
  // server's newest-first ordering, and a document whose newest change is not
  // history[0] would crown the wrong row.
  const isCurrent = (entry: AgreementHistoryEntry) => entry.version === data?.version;

  return (
    <Modal open onClose={onClose} title={AGREEMENT_COPY.historyTitle}>
      <p className="-mt-2 mb-4 text-xs text-muted">{AGREEMENT_COPY.historySubtitle}</p>

      {data === undefined ? (
        // On the real screen this branch is a formality: the page above has
        // already answered this query and this modal reads the same key from
        // the cache. It exists because a component may not assume a warm
        // cache, and rendering historyEmpty while the answer is still in
        // flight would tell a household with years of history that it has
        // none -- the vacuous-first-render defect the spec names for `locked`.
        <p className="text-xs text-muted">
          {isLoading ? AGREEMENT_COPY.loading : AGREEMENT_COPY.historyEmpty}
        </p>
      ) : (
        // The content block scrolls, not the panel -- the exact shape of
        // VisionModal.tsx's own max-h-[65vh] content block. Do NOT reach for
        // the design's `hb-scroll` class; it does not exist in web/src.
        <div className="flex max-h-[65vh] flex-col gap-1 overflow-y-auto pr-1">
          {history.length === 0 ? (
            <p className="text-xs text-muted">{AGREEMENT_COPY.historyEmpty}</p>
          ) : (
            <ul className="flex flex-col gap-1">
              {shown.map((entry) => (
                <li
                  key={entry.proposalId}
                  data-testid={`agreement-history-${entry.proposalId}`}
                  className="rounded-[10px] border border-hairline p-3"
                >
                  <div className="flex items-center gap-2">
                    <span
                      aria-hidden="true"
                      className={`h-2 w-2 flex-none rounded-full ${
                        isCurrent(entry) ? "bg-accent" : "bg-hairline"
                      }`}
                    />
                    <span className="text-[12.5px] font-semibold text-ink">
                      {isCurrent(entry)
                        ? AGREEMENT_COPY.historyVersionCurrent(entry.version)
                        : AGREEMENT_COPY.historyVersion(entry.version)}
                    </span>
                    <span className="ml-auto text-[11.5px] text-muted">
                      {historyDateLabel(entry.acceptedAt)}
                    </span>
                  </div>

                  {/* A removal's own wording is `previousBody` -- its `body` is
                      empty by constraint -- and that is the copy the design
                      promises stays readable here. */}
                  <p className="mt-1.5 text-[13px] leading-relaxed text-ink">
                    {AGREEMENT_COPY.historySentence(
                      entry.kind,
                      entry.sectionName,
                      entry.kind === "remove" ? entry.previousBody : entry.body,
                    )}
                  </p>

                  <div className="mt-1.5 flex items-center justify-between gap-3">
                    <span className="text-[11.5px] text-muted">
                      {AGREEMENT_COPY.historySignedBy(entry.signedByNames)}
                    </span>
                    {entry.kind === "remove" && (
                      // Removals only. An edit entry gets no Restore: that is
                      // an edit back, one click away through the ordinary flow.
                      // The section always still exists (sections are never
                      // removed) and may be empty and invisible, which the
                      // propose picker still offers.
                      <button
                        type="button"
                        onClick={() =>
                          onRestore({
                            mode: "add",
                            body: entry.previousBody,
                            sectionId: entry.sectionId,
                          })
                        }
                        className="min-h-11 flex-none rounded-lg border border-hairline px-3 text-[12px] font-semibold text-accent sm:min-h-0 sm:py-1.5"
                      >
                        {AGREEMENT_COPY.historyRestore}
                      </button>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}

          {collapsed.length > 0 && !expanded && (
            // A real button, and the reveal fetches nothing: everything it
            // shows is already in `history`.
            <button
              type="button"
              onClick={() => setExpanded(true)}
              className="min-h-11 self-start text-[12.5px] font-semibold text-accent sm:min-h-0"
            >
              {AGREEMENT_COPY.historyShowOlder(lowest, highest)}
            </button>
          )}
        </div>
      )}
    </Modal>
  );
}
