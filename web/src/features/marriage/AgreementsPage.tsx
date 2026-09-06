// The Agreements screen: the header every state shares, the four states that
// come before there is a document to render, and the page state Tasks 11-15
// mount against. Composition only, the RetrosPage.tsx/VisionPage.tsx
// convention -- fetch orchestration lives in useAgreements.ts and no apiFetch
// call belongs here.
//
// This page owns EVERY piece of state the feature's modals need: the session,
// the three modal slots and the three header buttons that fill them. A modal
// that owned its own open flag would still need a button somewhere else to set
// it, and the button and the flag would then live in two files. Tasks 13-15
// each add one modal and bind one already-existing value.
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { ApiError } from "../../api/client";
import { PageContainer } from "../../components/PageContainer";
import { useMe } from "../auth/useAuth";
import { AgreementSectionCard } from "./AgreementSectionCard";
import { NewSectionModal } from "./NewSectionModal";
import { ProposalCard } from "./ProposalCard";
import { ProposeAgreementModal, type AgreementProposeSeed } from "./ProposeAgreementModal";
import { VersionHistoryModal } from "./VersionHistoryModal";
import { AGREEMENT_COPY, agreementDateLabel } from "./agreementCopy";
import { handleWriteError, useAgreements } from "./useAgreements";

const PANEL = "rounded-xl border border-hairline bg-card p-[22px]";
// min-h-11 is the 44px touch-target floor (CLAUDE.md); inline-flex
// items-center because a <a> renders inline and would otherwise pin its text
// to the top of the grown box, the same reason Sidebar.tsx's NAV_ITEM_CLASS
// gives.
const CTA =
  "inline-flex min-h-11 items-center rounded-lg bg-accent px-3.5 text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60";
const HEADER_BUTTON =
  "inline-flex min-h-11 items-center rounded-lg border border-hairline bg-card px-3.5 text-[13px] text-ink";
const MUTED = "mt-1.5 text-[13px] text-muted";

export function AgreementsPage() {
  const agreements = useAgreements();
  // The page owns the session. Task 13's propose modal names the co-owners who
  // will be asked to agree ("Christine will be asked to agree before it takes
  // effect"), which needs both the owners list and which of them is the
  // viewer; a useMe() inside the modal would be a second subscriber to the
  // same query for one sentence.
  const me = useMe();

  // The three modal slots. Each task binds one already-declared value and
  // mounts its modal at the marked point at the bottom of this file. The
  // buttons, the state and its setters land here so a modal task adds a modal
  // and nothing else.
  const [proposeSeed, setProposeSeed] = useState<AgreementProposeSeed | null>(null);
  const [newSectionOpen, setNewSectionOpen] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);

  // The one write this task makes. One flag and one error slot, not a
  // per-button pair: there is exactly one starter-set button on screen at a
  // time (RetrosPage.tsx's own precedent for its single Start control).
  const [seeding, setSeeding] = useState(false);
  const [seedError, setSeedError] = useState<string | null>(null);

  function handleStarterSet() {
    setSeeding(true);
    setSeedError(null);
    agreements
      .seedStarterSet()
      .catch((err: unknown) =>
        setSeedError(handleWriteError(err, agreements.reload, AGREEMENT_COPY.starterSetError)),
      )
      .finally(() => setSeeding(false));
  }

  // Both queries. The header this page paints carries a control that opens a
  // modal built from the session (Task 13), so painting it before /auth/me has
  // answered would offer a button whose modal has no names in it.
  if (agreements.isLoading || me.isLoading) {
    return <p className="p-9 text-xs text-muted">{AGREEMENT_COPY.loading}</p>;
  }

  if (agreements.error) {
    // The real status, never a second useMe() role check that could disagree
    // with what the server decided. Being told a screen is owner-only is not an
    // incident, so this half is a plain <section> and only the half below it is
    // an alert -- the gap BillsPage.tsx shipped without, docs/LEARNING.md
    // pattern 1, and the shape RetrosPage.tsx:79-88 already carries.
    const status = agreements.error instanceof ApiError ? agreements.error.status : undefined;
    if (status === 403) {
      return (
        <section data-testid="agreements-owner-only" className={`m-9 ${PANEL}`}>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">
            {AGREEMENT_COPY.title}
          </h1>
          <h2 className="mt-4 text-xs text-muted">{AGREEMENT_COPY.ownerOnlyHeading}</h2>
          <p className="mt-1.5 text-[13px] text-ink">{AGREEMENT_COPY.ownerOnlyBody}</p>
        </section>
      );
    }
    return (
      <p role="alert" data-testid="agreements-load-error" className="p-9 text-xs text-danger">
        {AGREEMENT_COPY.loadError}
      </p>
    );
  }

  // Guards the type only (RetrosPage.tsx:96-98): error is null and isLoading is
  // false here, so data is present.
  if (!agreements.data) {
    return null;
  }

  const doc = agreements.data;
  // Whose names the modal prints, and nothing more. Permission is the server's:
  // Agree and Withdraw read the stamped canAgree/canWithdraw, never a membership
  // id compared in the browser (docs/LEARNING.md pattern 1).
  const coOwnerNames = doc.owners
    .filter((owner) => owner.membershipId !== me.data?.membership.id)
    .map((owner) => owner.name);
  // Every write refuses while the household is locked (decision 22), so a
  // proposal, a history row or a live agreement PROVES this household once had
  // two owners. Sections alone do not: a section is a label, not a promise
  // (decision 8), and "Use starter set" is the one thing a locked household
  // could not have clicked either -- but a household that was unlocked, seeded
  // and then lost an owner has sections with nothing in them, which is the case
  // count > 0 keeps on the right side of this line.
  const hasContent =
    doc.proposals.length > 0 || doc.history.length > 0 || doc.sections.some((s) => s.count > 0);
  // The service's own per-section counts -- never `version - 1`, and never a
  // second total the wire would have to keep honest.
  const nothingAgreed = doc.sections.every((section) => section.count === 0);
  // GONE, not disabled (decision 3 and the spec's own wording). A disabled
  // button that cannot say why is the defect the admin flags screen already
  // carries.
  const canWrite = !doc.locked;
  // Version history stays in a locked household, being a read -- but only once
  // there is something to read. A household that never had two owners has an
  // empty history and no rows behind the button.
  const showHistory = !doc.locked || hasContent;

  // The display number of whatever an edit or a remove targets, as the
  // document numbers it right now -- null when no live agreement carries that
  // id, which is the targetChanged case and the reason the card can say so.
  // Composed here, from the same array the grid renders, so a card and a row
  // can never disagree about what "Money 02" means.
  const liveAgreements = doc.sections.flatMap((section) => section.agreements);
  const targetNumberOf = (targetAgreementId: string) =>
    liveAgreements.find((agreement) => agreement.id === targetAgreementId)?.number ?? null;

  // `visible` is the server's own flag (decision 8), never a rule re-derived
  // here; the propose picker (Task 13) is handed doc.sections whole, empty
  // sections included, off this same array.
  const visible = doc.sections.filter((section) => section.visible);
  // Column-MAJOR, and deliberately not `grid-cols-2`: a row-major fill would
  // put Conflict's 03-05 beside Money's 01-02 and zig-zag the design's
  // continuous numbering down the page. Below `lg` the two wrappers stack, so
  // server order (created_at, id -- decision 11) holds at every width.
  const half = Math.ceil(visible.length / 2);
  const columns = [visible.slice(0, half), visible.slice(half)];

  return (
    <PageContainer data-testid="agreements-page">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">
            {AGREEMENT_COPY.title}
          </h1>
          {/* The version clause appends only once updatedAt is a real moment,
              never "v1, updated —". Task 11 renders the document below this
              header and leaves this element, and its testid, alone. */}
          <p data-testid="agreements-subtitle" className="mt-1 text-[13px] text-muted">
            {AGREEMENT_COPY.subtitle}
            {doc.updatedAt !== null &&
              ` · ${AGREEMENT_COPY.versionClause(doc.version, agreementDateLabel(doc.updatedAt))}`}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {/* Not a button -- nothing on it opens anything (the design's own
              static badge), so the 44px floor for interactive controls does not
              apply. RetrosPage.tsx:124-129's classes verbatim. */}
          <span
            data-testid="agreements-privacy-badge"
            className="inline-flex items-center rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] text-muted"
          >
            {AGREEMENT_COPY.privacyBadge}
          </span>
          {canWrite && (
            <button
              type="button"
              data-testid="agreements-new-section"
              aria-haspopup="dialog"
              onClick={() => setNewSectionOpen(true)}
              className={HEADER_BUTTON}
            >
              {AGREEMENT_COPY.newSection}
            </button>
          )}
          {showHistory && (
            <button
              type="button"
              data-testid="agreements-history"
              aria-haspopup="dialog"
              onClick={() => setHistoryOpen(true)}
              className={HEADER_BUTTON}
            >
              {AGREEMENT_COPY.versionHistory}
            </button>
          )}
          {canWrite && (
            <button
              type="button"
              data-testid="agreements-propose"
              aria-haspopup="dialog"
              // "add" rather than a hardcoded "edit": the chips default from
              // the seed, and the header's own entry point is a new agreement.
              onClick={() => setProposeSeed({ mode: "add" })}
              className={CTA}
            >
              {AGREEMENT_COPY.proposeChange}
            </button>
          )}
        </div>
      </div>

      {doc.locked && !hasContent && (
        <section data-testid="agreements-locked-invite" className={PANEL}>
          <p aria-hidden="true" className="text-[28px]">
            {AGREEMENT_COPY.lockedTile}
          </p>
          <h2 className="mt-2 text-sm font-semibold text-ink">{AGREEMENT_COPY.lockedHeadline}</h2>
          <p className={MUTED}>{AGREEMENT_COPY.lockedBody}</p>
          <p className={MUTED}>{AGREEMENT_COPY.lockedOwnerCount}</p>
          {/* Into the existing Settings invite flow, not a second invite
              implementation (decision 2). settingsRoute's validateSearch, added
              in Step 7 below, is what makes this `search` prop typecheck at all
              -- which is why the route and this page land in one commit. */}
          <Link to="/settings" search={{ invite: true }} className={`mt-4 ${CTA}`}>
            {AGREEMENT_COPY.invitePartner}
          </Link>
        </section>
      )}

      {doc.locked && hasContent && (
        <section data-testid="agreements-frozen" className={PANEL}>
          <p className="text-[13px] text-ink">{AGREEMENT_COPY.frozenBanner}</p>
          {doc.proposals.length > 0 && (
            <p className={MUTED}>{AGREEMENT_COPY.frozenProposals(doc.proposals.length)}</p>
          )}
        </section>
      )}

      {!doc.locked && doc.sections.length === 0 && (
        <section data-testid="agreements-empty" className={PANEL}>
          <h2 className="text-sm font-semibold text-ink">{AGREEMENT_COPY.emptyHeadline}</h2>
          <p className={MUTED}>{AGREEMENT_COPY.emptyBody}</p>
          {/* Opens New section, not Propose: Propose's section select would be
              empty here, the BillsPage dead end docs/LEARNING.md records, on
              the first screen anyone sees. Above "Use starter set", the
              design's own order. */}
          <button
            type="button"
            data-testid="agreements-add-first"
            onClick={() => setNewSectionOpen(true)}
            className={`mt-4 ${CTA}`}
          >
            {AGREEMENT_COPY.addFirstAgreement}
          </button>
          <button
            type="button"
            data-testid="agreements-starter-set"
            onClick={handleStarterSet}
            disabled={seeding}
            className={`mt-4 ${CTA}`}
          >
            {AGREEMENT_COPY.useStarterSet}
          </button>
          {seedError && (
            <p role="alert" className="mt-2 text-xs text-danger">
              {seedError}
            </p>
          )}
          <h3 className="mt-5 text-xs text-muted">{AGREEMENT_COPY.popularHeading}</h3>
          {/* Read-only illustration: the design draws these four with hover
              styling and no onClick, and nothing says what tapping one would
              propose. <li>, not <button>, so nothing suggests otherwise. */}
          <ul className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
            {AGREEMENT_COPY.popularCards.map((name) => (
              <li key={name} className={`${PANEL} text-[13px] text-ink`}>
                {name}
              </li>
            ))}
          </ul>
        </section>
      )}

      {!doc.locked && doc.sections.length > 0 && nothingAgreed && (
        <section data-testid="agreements-seeded" className={PANEL}>
          <h2 className="text-sm font-semibold text-ink">{AGREEMENT_COPY.seededHeadline}</h2>
          <p className={MUTED}>{AGREEMENT_COPY.seededBody(doc.sections.map((s) => s.name))}</p>
          {/* The only call to action here -- "Use starter set" is gone by this
              state, sections already existing. Opens Propose on the first
              section rather than New section: there is now something for the
              picker to offer. */}
          <button
            type="button"
            data-testid="agreements-add-first"
            onClick={() => setProposeSeed({ mode: "add", sectionId: doc.sections[0].id })}
            className={`mt-4 ${CTA}`}
          >
            {AGREEMENT_COPY.addFirstAgreement}
          </button>
        </section>
      )}

      {/* One block above the sections, not the design's right column: below
          `lg` there is one column, and a household with no sections at all
          still has to see what is waiting for it. Every write goes through
          handleWriteError, which owns the refetch that turns a 409 into a
          card that explains itself -- so each handler resolves to null when
          the write landed, or to the sentence the card shows. */}
      {doc.proposals.length > 0 && (
        <div data-testid="agreements-proposals" className="flex flex-col gap-4">
          {doc.proposals.map((proposal) => (
            <ProposalCard
              key={proposal.id}
              proposal={proposal}
              targetNumber={targetNumberOf(proposal.targetAgreementId)}
              onAgree={(id) =>
                agreements
                  .agree(id)
                  .then(() => null)
                  .catch((err: unknown) => handleWriteError(err, agreements.reload, AGREEMENT_COPY.agreeError))
              }
              onPark={(id, note) =>
                agreements
                  .park(id, note)
                  .then(() => null)
                  .catch((err: unknown) => handleWriteError(err, agreements.reload, AGREEMENT_COPY.parkError))
              }
              onWithdraw={(id) =>
                agreements
                  .withdraw(id)
                  .then(() => null)
                  .catch((err: unknown) => handleWriteError(err, agreements.reload, AGREEMENT_COPY.withdrawError))
              }
            />
          ))}
        </div>
      )}

      {visible.length > 0 && (
        <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
          {columns.map((column, i) => (
            <div key={i} data-testid={`agreements-column-${i}`} className="flex flex-col gap-4">
              {column.map((section) => (
                <AgreementSectionCard key={section.id} section={section} />
              ))}
            </div>
          ))}
        </div>
      )}

      {proposeSeed && (
        <ProposeAgreementModal
          seed={proposeSeed}
          coOwnerNames={coOwnerNames}
          sections={doc.sections}
          onOpenNewSection={() => {
            // Close, then open: nothing here stacks <dialog>s, and closing this one
            // is also what discards its draft and its latch.
            setProposeSeed(null);
            setNewSectionOpen(true);
          }}
          onClose={() => setProposeSeed(null)}
        />
      )}

      {newSectionOpen && (
        <NewSectionModal
          onClose={() => setNewSectionOpen(false)}
          onCreated={(seed) => {
            // Close, then open. Both booleans are Task 10's; this is the swap.
            setNewSectionOpen(false);
            setProposeSeed(seed);
          }}
        />
      )}

      {historyOpen && (
        <VersionHistoryModal
          onClose={() => setHistoryOpen(false)}
          onRestore={(seed) => {
            // Close, then open -- the same swap New section makes into
            // Propose. Restoring is an ordinary add proposal (decision 18),
            // not a second write path.
            setHistoryOpen(false);
            setProposeSeed(seed);
          }}
        />
      )}
    </PageContainer>
  );
}
