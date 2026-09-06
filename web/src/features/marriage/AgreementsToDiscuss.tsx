// The read-only "To discuss" block the Retros page mounts (decision 7). Same
// GET, same hook and same query key as the Agreements page, so one Agree here
// refreshes both screens off a single invalidation. Frontend composition only:
// nothing writes into a retro table and no proposal links to a retro row --
// the next retro usually does not exist yet, which is the whole reason there is
// no foreign key to add.
import { useState } from "react";
import { AGREEMENT_COPY, proposalSummary } from "./agreementCopy";
import { handleWriteError, useAgreements } from "./useAgreements";

export function AgreementsToDiscuss() {
  const agreements = useAgreements();
  // One in-flight slot and one error slot, not a pair per row: only one Agree
  // is ever in flight here (RetrosPage.tsx's own starting/startError pair).
  const [agreeingId, setAgreeingId] = useState<string | null>(null);
  const [agreeError, setAgreeError] = useState<string | null>(null);

  function handleAgree(id: string) {
    setAgreeingId(id);
    setAgreeError(null);
    // handleWriteError refetches on a 409 itself: this block holds no draft, so
    // the refreshed document IS the fix -- hence no hadConflict latch, which
    // belongs to the propose modal, the one surface with typing to lose.
    agreements
      .agree(id)
      .catch((err: unknown) =>
        setAgreeError(handleWriteError(err, agreements.reload, AGREEMENT_COPY.agreeError)),
      )
      .finally(() => setAgreeingId(null));
  }

  // Silence while loading: a heading with no rows claims something is parked
  // before anything says what. Muted on error, never role="alert" -- this block
  // is a guest on the Retros page and must not look like the page broke.
  if (agreements.isLoading) return null;
  if (agreements.error !== null && agreements.error !== undefined) {
    return (
      <p data-testid="agreements-to-discuss-error" className="text-xs text-muted">
        {AGREEMENT_COPY.toDiscussLoadError}
      </p>
    );
  }

  const doc = agreements.data;
  const parked = doc?.proposals.filter((p) => p.status === "parked") ?? [];
  if (parked.length === 0) return null;

  // The target's display number as the document numbers it right now, null when
  // no live agreement carries that id -- the same derivation AgreementsPage
  // makes for its own cards. The wire carries no number on a proposal:
  // numbering is the document's and derived at render (decision 11).
  const numberOf = (targetAgreementId: string): number | null =>
    doc?.sections.flatMap((s) => s.agreements).find((a) => a.id === targetAgreementId)?.number ??
    null;

  return (
    <section
      data-testid="agreements-to-discuss"
      className="rounded-xl border border-hairline bg-card p-[22px]"
    >
      <h2 className="text-sm font-semibold text-ink">{AGREEMENT_COPY.toDiscussTitle}</h2>
      <p className="mt-1 text-xs text-muted">{AGREEMENT_COPY.toDiscussSubtitle}</p>
      {agreeError && (
        <p role="alert" className="mt-2 text-xs text-danger">
          {agreeError}
        </p>
      )}
      <ul className="mt-4 flex flex-col gap-3">
        {parked.map((p) => (
          <li
            key={p.id}
            data-testid={`to-discuss-row-${p.id}`}
            className="flex flex-wrap items-start justify-between gap-3 border-t border-hairline pt-3 first:border-t-0 first:pt-0"
          >
            <div className="min-w-0">
              <p className="text-[13px] text-ink">
                {proposalSummary(p.kind, p.proposedByName, p.sectionName, numberOf(p.targetAgreementId))}
              </p>
              {/* An edit shows what it would replace; an add has no previousBody
                  and a remove has no body, so this reads the fields rather than
                  asking the kind a second time. */}
              {p.previousBody !== "" && (
                <p className="mt-0.5 text-[12.5px] text-muted line-through">{p.previousBody}</p>
              )}
              {p.body !== "" && <p className="mt-0.5 text-[12.5px] text-ink">{p.body}</p>}
              {/* An empty park note is ordinary: Discuss is a bare button. */}
              {p.parkNote !== "" && <p className="mt-1 text-[12.5px] text-muted">{p.parkNote}</p>}
            </div>
            {/* canAgree is the server's own flag (!locked && …), so a locked
                household loses this button with no second rule in the browser to
                disagree with it (decisions 3 and 16). No Discuss and no
                Withdraw: a reminder, not a second editing surface. min-h-11 is
                the 44px touch floor. */}
            {p.canAgree && (
              <button
                type="button"
                onClick={() => handleAgree(p.id)}
                disabled={agreeingId === p.id}
                className="min-h-11 rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
              >
                {AGREEMENT_COPY.agree}
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
