// One open proposal -- pending or parked -- and the actions the server says
// this viewer may take. Mutations arrive as props, never a useAgreements()
// call per card: a hook per card is several callers racing one cache entry.
import { useState } from "react";
import { AGREEMENT_COPY, agreementDateLabel, proposalSummary } from "./agreementCopy";
import type { AgreementProposal } from "./agreementSchemas";

export type ProposalCardProps = {
  proposal: AgreementProposal;
  // The target's display number as the document numbers it right now, and null
  // when no live agreement carries that id -- the targetChanged case. The wire
  // carries no number on a proposal (decision 11).
  targetNumber: number | null;
  // Each resolves to null when the write landed, or to the sentence to show.
  // The page has already run the failure through handleWriteError, which owns
  // the refetch that turns a 409 AGREEMENT_CHANGED into a targetChanged card.
  onAgree: (proposalId: string) => Promise<string | null>;
  onPark: (proposalId: string, note: string) => Promise<string | null>;
  onWithdraw: (proposalId: string) => Promise<string | null>;
};

type Action = "agree" | "park" | "withdraw";

// Six buttons and every text line share these, so a change lands once. Each is
// a COMPLETE string on purpose: two Tailwind utilities for the same property
// on one element resolve by stylesheet order rather than by the order they are
// written, so `${primary} bg-danger` is a coin toss between accent and danger.
// RetroModal.tsx:640-648 spells its own danger button out for the same reason.
const primary =
  "min-h-11 rounded-lg bg-accent px-3.5 py-2 text-xs font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0";
const secondary =
  "min-h-11 rounded-lg border border-callout-border bg-card px-3.5 py-2 text-xs font-semibold text-label disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0";
const dangerGhost =
  "min-h-11 rounded-lg border border-callout-border bg-card px-3.5 py-2 text-xs font-semibold text-danger disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0";
const dangerPrimary =
  "min-h-11 flex-1 rounded-lg bg-danger px-3.5 py-2 text-xs font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0";
const box = "mt-3 rounded-[10px] border border-hairline bg-card p-3";
const alertLine = "mt-2 text-xs leading-snug text-danger";
// Colourless: every line below adds its own colour, for the same
// two-utilities-one-property reason as the buttons.
const line = "mt-1.5 text-[12.5px] leading-[1.5]";

export function ProposalCard({ proposal, targetNumber, onAgree, onPark, onWithdraw }: ProposalCardProps) {
  const [discussing, setDiscussing] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [note, setNote] = useState("");
  // ONE busy value for the card: nothing here can start a second write while
  // one is in flight. Each action keeps its own error line, because the
  // sentence belongs under the button that produced it.
  const [busy, setBusy] = useState<Action | null>(null);
  const [errors, setErrors] = useState({ agree: "", park: "", withdraw: "" });

  // A REFUSING default: doc.proposals excludes accepted and withdrawn in SQL,
  // so a fourth status here is a row nothing wrote. Returning no card beats
  // half a card under a guessed title -- and the schema's status enum stays
  // four-valued, because a write response carries the row at the status it now
  // holds and nothing renders that field.
  let title: string;
  switch (proposal.status) {
    case "pending":
      title = AGREEMENT_COPY.pendingTitle(proposal.awaitingNames);
      break;
    case "parked":
      title = AGREEMENT_COPY.parkedTitle(proposal.awaitingNames);
      break;
    default:
      return null;
  }

  async function run(action: Action, write: () => Promise<string | null>): Promise<boolean> {
    setBusy(action);
    setErrors((prev) => ({ ...prev, [action]: "" }));
    const message = await write();
    setBusy(null);
    if (message !== null) setErrors((prev) => ({ ...prev, [action]: message }));
    return message === null;
  }

  async function park() {
    // Only a landed park closes the panel and clears the note: on a failure the
    // typed sentence is still there to send again.
    if (await run("park", () => onPark(proposal.id, note))) {
      setDiscussing(false);
      setNote("");
    }
  }

  // canAgree ALONE decides the Agree button. It already carries
  // `!locked && (!signedByViewer || awaitingNames is empty)`, so
  // `awaitingNames.length === 0 || canAgree` would offer Agree on a locked
  // household whose every write refuses with 409 (decisions 3 and 16).
  const everyoneAgreed = proposal.canAgree && proposal.awaitingNames.length === 0;
  // No un-park: parking says where the conversation goes, not a state to undo
  // before signing. And nobody left to wait for means nothing left to discuss.
  const showDiscuss = proposal.canAgree && proposal.status === "pending" && proposal.awaitingNames.length > 0;
  const staleNote = proposal.canWithdraw
    ? AGREEMENT_COPY.staleMine
    : proposal.proposedByName === ""
      ? AGREEMENT_COPY.staleNoName
      : AGREEMENT_COPY.staleTheirs(proposal.proposedByName);

  return (
    <div
      data-testid={`agreement-proposal-${proposal.id}`}
      className="rounded-xl border border-callout-border bg-callout px-5 py-[18px]"
    >
      <p className="text-[13px] font-semibold text-accent">{title}</p>
      <p className={`${line} text-ink`}>
        {proposalSummary(proposal.kind, proposal.proposedByName, proposal.sectionName, targetNumber)}
        <span className="text-muted"> · {agreementDateLabel(proposal.proposedAt)}</span>
      </p>
      {/* Which of previousBody/body renders is the kind's own shape on the wire
          -- an add carries no previousBody, a remove no body -- so this reads
          the fields. Only the strike-through asks the kind: an edit is the one
          card that shows both wordings, which is the only place a signer sees
          what they are agreeing to change. */}
      {proposal.previousBody !== "" && (
        <p
          data-testid="proposal-previous-body"
          className={proposal.kind === "edit" ? `${line} text-muted line-through` : `${line} text-ink`}
        >
          {proposal.previousBody}
        </p>
      )}
      {proposal.body !== "" && (
        <p data-testid="proposal-body" className={`${line} text-ink`}>
          {proposal.body}
        </p>
      )}
      {proposal.note !== "" && <p className={`${line} text-muted`}>{proposal.note}</p>}
      {/* An empty park note is ordinary, not missing data: Discuss is a bare
          button and decision 7 stores whatever was typed, including nothing. */}
      {proposal.status === "parked" && proposal.parkNote !== "" && (
        <p data-testid="proposal-park-note" className={`${line} text-muted`}>
          <span className="font-semibold text-ink">{AGREEMENT_COPY.toDiscuss}</span> {proposal.parkNote}
        </p>
      )}
      {proposal.targetChanged && (
        <p data-testid="proposal-stale-note" className={`${line} text-danger`}>
          {staleNote}
        </p>
      )}
      {everyoneAgreed && (
        <p data-testid="proposal-everyone-agreed" className={`${line} text-muted`}>
          {AGREEMENT_COPY.everyoneAgreed}
        </p>
      )}
      <div className="mt-3 flex flex-wrap gap-2">
        {proposal.canAgree && (
          <button
            type="button"
            disabled={proposal.targetChanged || busy !== null}
            onClick={() => void run("agree", () => onAgree(proposal.id))}
            className={primary}
          >
            {AGREEMENT_COPY.agree}
          </button>
        )}
        {showDiscuss && (
          <button type="button" disabled={busy !== null} onClick={() => setDiscussing(true)} className={secondary}>
            {AGREEMENT_COPY.discuss}
          </button>
        )}
        {proposal.canWithdraw && !confirming && (
          <button type="button" disabled={busy !== null} onClick={() => setConfirming(true)} className={dangerGhost}>
            {AGREEMENT_COPY.withdraw}
          </button>
        )}
      </div>
      {errors.agree !== "" && (
        <p role="alert" className={alertLine}>
          {errors.agree}
        </p>
      )}
      {/* Discuss expands HERE, inside the card, because decision 7 stores a
          park note that a bare button would leave empty every time. */}
      {discussing && (
        <div data-testid="proposal-discuss" className={box}>
          <label htmlFor={`park-note-${proposal.id}`} className="text-xs text-label">
            {AGREEMENT_COPY.parkNoteLabel}
          </label>
          {/* maxLength mirrors MaxAgreementParkNoteLen as a courtesy only: the
              browser counts UTF-16 units and the server's runes decide. */}
          <textarea
            id={`park-note-${proposal.id}`}
            value={note}
            onChange={(event) => setNote(event.target.value)}
            rows={3}
            maxLength={500}
            className="mt-1 w-full rounded-lg border border-hairline bg-card p-2 text-[13px] text-ink"
          />
          <div className="mt-2.5 flex gap-2.5">
            <button type="button" onClick={() => setDiscussing(false)} className={`${secondary} flex-1`}>
              {AGREEMENT_COPY.cancel}
            </button>
            <button type="button" disabled={busy !== null} onClick={() => void park()} className={`${primary} flex-1`}>
              {AGREEMENT_COPY.parkAction}
            </button>
          </div>
          {errors.park !== "" && (
            <p role="alert" className={alertLine}>
              {errors.park}
            </p>
          )}
        </div>
      )}
      {/* The same two-button confirm RetroModal.tsx:633-671 uses, never
          window.confirm: a browser dialog cannot be styled, cannot be tested
          without stubbing a global, and blocks the tab it opens on. */}
      {confirming && (
        <div data-testid="proposal-withdraw-confirm" className={box}>
          <p className="text-[12.5px] text-ink">{AGREEMENT_COPY.withdrawConfirmBody}</p>
          <div className="mt-2.5 flex gap-2.5">
            <button type="button" onClick={() => setConfirming(false)} className={`${secondary} flex-1`}>
              {AGREEMENT_COPY.cancel}
            </button>
            <button
              type="button"
              disabled={busy !== null}
              onClick={() => void run("withdraw", () => onWithdraw(proposal.id))}
              className={dangerPrimary}
            >
              {AGREEMENT_COPY.withdrawConfirmAction}
            </button>
          </div>
          {errors.withdraw !== "" && (
            <p role="alert" className={alertLine}>
              {errors.withdraw}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
