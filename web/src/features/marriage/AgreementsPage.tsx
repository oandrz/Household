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
import { AGREEMENT_COPY, agreementDateLabel } from "./agreementCopy";
import type { AgreementKind } from "./agreementSchemas";
import { handleWriteError, useAgreements } from "./useAgreements";

// One seed for all four of the Propose modal's entry points -- the header
// button, the New-section modal's "Create & add first agreement", a section
// card's own add, and version history's Restore. Every field is a pre-fill,
// so the modal never has to know which door the caller came through. `mode`
// reuses the wire's own kind enum rather than restating the union: two literal
// unions for one server enum is how the two drift apart.
//
// Written inline rather than as an exported type, because the name belongs to
// ProposeAgreementModal.tsx (Task 13) and this file already imports that one:
// exporting a second name here would either duplicate the union or make the
// modal import the page back. Task 13 replaces this annotation with the
// imported `AgreementProposeSeed`.

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

  // The three modal slots. The VALUES are elided here because nothing reads
  // them until their modal exists -- tsconfig has noUnusedLocals, so a bound
  // name with no reader would not compile. Each task below binds its own:
  //   Task 13 -> const [proposeSeed, setProposeSeed]
  //   Task 14 -> const [newSectionOpen, setNewSectionOpen]
  //   Task 15 -> const [historyOpen, setHistoryOpen]
  // and mounts its modal at the marked point at the bottom of this file. The
  // buttons, the state and its setters land here so a modal task adds a modal
  // and nothing else.
  const [, setProposeSeed] = useState<{
    mode: AgreementKind;
    sectionId?: string;
    targetAgreementId?: string;
    body?: string;
  } | null>(null);
  const [, setNewSectionOpen] = useState(false);
  const [, setHistoryOpen] = useState(false);

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
          {/* "Add your first agreement" belongs beside this button and lands in
              Task 14 with the New-section modal it opens -- on this screen it
              must open New section, not Propose, because Propose's section
              select would be empty (the BillsPage dead end docs/LEARNING.md
              records) on the first screen anyone sees. Every control that opens
              a modal lands with that modal. */}
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
        </section>
      )}

      {/* Mount point. Task 12 renders doc.proposals as one block HERE, above
          the grid, for every state except the locked-invite one. Tasks 13, 14
          and 15 mount their modals at the very end, each binding the state
          slot reserved for it at the top of this file. All four states above
          stay exactly as they are. */}

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
    </PageContainer>
  );
}
