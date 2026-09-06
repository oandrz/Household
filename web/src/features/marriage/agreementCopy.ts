// Every user-visible string on the Agreements screen, its three modals and
// helpers used across it, kept in one plain .ts module for the reason
// visionCopy.ts:1-5 gives (eslint's react-refresh/only-export-components
// never has to think about a file that mixes components with other exports,
// and every user-facing string lives in exactly one place).
import type { AgreementProposal } from "./agreementSchemas";

// "Christine"; "Andreas and Christine"; "Andreas, Bev and Christine". One
// function for four call sites -- the proposal card's "needs …", version
// history's "Agreed by …", the propose modal's subtitle and the seeded
// sentence -- so three owners cannot read correctly on one screen and wrongly
// on the next. "and", not the design's "&": one caller joins section NAMES,
// and "Home & kids & Us" reads as three sections rather than two.
export function joinNames(names: string[]): string {
  if (names.length === 0) return "";
  if (names.length === 1) return names[0];
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
}

// The card's one-line summary, composed from the fields that exist because
// there is no summary column on the wire. The target reads "Money 02" when the
// document still numbers it and just "Money" when it does not (targetChanged),
// since the number is the document's and derived at render (decision 11).
//
// Attribution falls back to "Proposed ..." when proposedByName is "" -- the
// membership no longer resolves, which decision 20 makes ordinary for any
// household a partner has left, not a corruption.
//
// The default is fail-closed with no guess: agreementProposalSchema's z.enum
// already refused any other kind one layer up, and `const refused: never` is
// the compile-time proof that this switch covers the enum. It returns "" for
// that unreachable row rather than inventing a user-visible sentence -- and
// therefore invents no copy key either.
export function proposalSummary(
  kind: AgreementProposal["kind"],
  proposedByName: string,
  sectionName: string,
  targetNumber: number | null,
): string {
  const target = targetNumber === null ? sectionName : `${sectionName} ${String(targetNumber).padStart(2, "0")}`;
  const opening = (verb: string) => (proposedByName === "" ? `Proposed: ${verb}` : `${proposedByName} proposed ${verb}`);
  switch (kind) {
    case "add":
      return `${opening("adding to")} ${sectionName}:`;
    case "edit":
      return `${opening("changing")} ${target}:`;
    case "remove":
      return `${opening("removing")} ${target}:`;
    default: {
      const refused: never = kind;
      void refused;
      return "";
    }
  }
}

// "2026-06-28T21:18:52+08:00" -> "28 Jun", the design's header wording. The
// wire carries a full timestamp with its own offset, so none of monthNameOnly's
// UTC-midnight caution applies (retroCopy.ts:289-297 has that reasoning, for
// the "2026-06" date-only strings retros uses instead).
export function agreementDateLabel(iso: string): string {
  return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

// The same date with its year -- "28 Jun 2026". Version history spans years by
// construction (nothing is ever deleted, so the log only grows), while a
// pending proposal is days old; two helpers rather than one flag, so neither
// call site has to remember which boolean means "with year".
export function historyDateLabel(iso: string): string {
  return new Date(iso).toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}

// Every user-visible string on the Agreements screen, its three modals and the
// Retros To-discuss block. ONE object: later tasks append keys under their own
// banner, they do not open a second export.
//
// Already taken by this task, and not to be re-added under another name:
//   subtitle, versionClause  -- Task 11 renders the same subtitle element this
//                               task built; it must not add `versionSuffix`
//                               or an AGREEMENT_DOCUMENT_COPY of its own.
//   useStarterSet            -- not `starterSet`.
//   seededHeadline, seededBody -- not `sectionsReadyTitle`/`sectionsReadyBody`.
//   sectionNameTaken         -- Task 14 reuses this one; it does not redeclare it.
// Reserved for Task 12, which appends them HERE: agree, discuss, withdraw,
// everyoneAgreed, pendingTitle, parkedTitle, cancel, parkNoteLabel.
export const AGREEMENT_COPY = {
  // --- The page, in every state (Task 10) --------------------------------
  title: "Our agreements",
  subtitle: "A living document — changes need every owner to agree",
  // Rendered in every state, including the populated one the design drops it
  // from: a household reading a page of private promises should be told it is
  // private on the screen that holds the most of them.
  privacyBadge: "🔒 Private — parents only",
  // Appended to the subtitle only once updatedAt is a real moment, never
  // "v1, updated —". Takes an already-formatted date rather than an ISO
  // string, so the caller chooses which of the two date labels applies.
  versionClause: (version: number, updated: string) => `v${version}, updated ${updated}`,
  loading: "Loading…",
  // Task 14 renders the button this names, in both unlocked empty states. The
  // key lands here because every string in this feature lives in one object.
  addFirstAgreement: "Add your first agreement",

  loadError: "Couldn't load your agreements.",
  // The routine "not the owner" refusal, told plainly and never as a red
  // alert (docs/LEARNING.md pattern 1) -- the exact gap BillsPage.tsx shipped
  // without, found again in BudgetPage.tsx and TransactionsPage.tsx.
  ownerOnlyHeading: "Owner only",
  ownerOnlyBody:
    "Agreements are visible to the household owner. Ask them if you'd like to see where things stand.",

  // The three header controls. Hidden -- not disabled -- when the document is
  // locked, except Version history, which stays because it is a read
  // (spec decision 3).
  newSection: "+ Section",
  versionHistory: "Version history",
  proposeChange: "Propose a change",

  // --- Locked, nothing written yet (decision 2) --------------------------
  lockedTile: "🤝",
  // Never "both of you": the signing set is every CURRENT owner (decision 4),
  // and nothing in this product caps a household at two.
  lockedHeadline: "Agreements need at least two owners",
  lockedBody:
    "Agreements are the promises your household lives by. Every one of them is agreed by all the owners before it takes effect.",
  lockedOwnerCount: "This household has one owner.",
  invitePartner: "Invite your partner",

  // --- Locked, with content (decision 3) ---------------------------------
  frozenBanner:
    "This household is down to one owner, so nothing here can change. Everything you agreed is still here, and it unlocks again when a second owner joins.",
  // So a household can see its frozen proposals were not deleted. The verb
  // agrees with the count, the way the Retros subtitle's already does.
  frozenProposals: (count: number) =>
    count === 1 ? "1 change is waiting for a second owner" : `${count} changes are waiting for a second owner`,

  // --- Empty, no sections ------------------------------------------------
  emptyHeadline: "Write your first agreements",
  emptyBody:
    "Agreements are the promises you keep to each other — how you handle money, conflict, home and time together. Each one is proposed by one owner and takes effect once every owner agrees.",
  useStarterSet: "Use starter set",
  popularHeading: "Popular starting points",
  // Read-only illustration. The design draws these four with hover styling and
  // no onClick, and nothing says what tapping one would propose -- so the
  // drawing ships and the interaction does not (spec, Out of scope). Held here
  // rather than as literals in the page so the four labels have one home.
  popularCards: ["Money", "Conflict", "Home & kids", "Us"],
  starterSetError: "Couldn't add the starter sections. Try again.",

  // --- Empty, sections seeded (decisions 8, 17) --------------------------
  // An empty section is invisible in the document, so a naively rendered
  // starter set changes nothing on screen. Naming the four in prose is what
  // proves the click worked, without needing an exception to decision 8.
  seededHeadline: "Your sections are ready",
  seededBody: (names: string[]) =>
    `${joinNames(names)} are ready. Nothing has been agreed yet — every agreement is proposed by one owner and takes effect once every owner agrees.`,

  // --- Write refusals, shown by handleWriteError (Task 9) ----------------
  writeErrorChanged:
    "This no longer matches the agreement it was written against, so nothing was signed.",
  writeErrorLocked: "This household is down to one owner, so nothing here can change.",
  writeErrorResolved: "That was already settled — this page has just refreshed.",
  sectionNameTaken: "You already have a section called that.",

  // "3 agreements" under the section name. The count is the service's own
  // (agreementSectionDTO.Count), never section.agreements.length: a total and
  // its breakdown that apply different filters quietly stop reconciling, which
  // browser criterion 7 exists to catch.
  sectionCount: (n: number) => (n === 1 ? "1 agreement" : `${n} agreements`),

  // --- ProposalCard (Task 12) ---------------------------------------------
  // The awaiting clause is dropped rather than left dangling: decision 16's
  // household has an empty list, and "Pending change — needs " is not a
  // sentence. joinNames generalises the design's literal "needs Christine" to
  // any number of owners (decision 4). Both dashes are em dashes (U+2014).
  pendingTitle: (awaiting: string[]) =>
    awaiting.length === 0 ? "Pending change" : `Pending change — needs ${joinNames(awaiting)}`,
  parkedTitle: (awaiting: string[]) =>
    awaiting.length === 0 ? "Parked for the next retro" : `Parked for the next retro — needs ${joinNames(awaiting)}`,
  everyoneAgreed: "Everyone still here has agreed — Agree once more to make it final.",
  toDiscuss: "To discuss",
  // Decision 14's three sentences. Which one shows is decided by canWithdraw,
  // never by "did I propose this": canWithdraw already carries decision 15's
  // fallback, so a household whose proposer has left is not told to ask a
  // ghost. staleNoName is that household's own case -- proposedByName is ""
  // when the membership no longer resolves (decision 20).
  staleMine:
    "This no longer matches the agreement it was written against, so it can't be agreed. Withdraw it and propose the change again.",
  staleTheirs: (proposer: string) =>
    `Ask ${proposer} to withdraw it and propose it again against the current wording.`,
  staleNoName: "This needs withdrawing and proposing again against the current wording.",
  agree: "Agree",
  discuss: "Discuss",
  withdraw: "Withdraw",
  // Introduced here and reused by Tasks 13-15 rather than redeclared: one
  // object, and a duplicate key is a TypeScript error.
  cancel: "Cancel",
  parkNoteLabel: "What you want to talk through (optional)",
  parkAction: "Park for next retro",
  withdrawConfirmBody:
    "Withdraw this proposal? It stops waiting for anyone, and nothing in the document changes.",
  withdrawConfirmAction: "Withdraw it",
  // The three fallbacks the page passes to handleWriteError -- shown only when
  // the failure is a genuine server failure rather than one of the refusals
  // the hook names.
  agreeError: "Couldn't agree that just now.",
  parkError: "Couldn't park that for the retro just now.",
  withdrawError: "Couldn't withdraw that just now.",

  // --- Propose modal (Task 13) ---
  proposeTitle: "Propose a change",
  changeTypeLabel: "Change type",
  modeAdd: "Add new",
  modeEdit: "Edit existing",
  modeRemove: "Remove",
  // `cancel` is NOT repeated here: Task 12 already added it to this object and a
  // duplicate key is a TypeScript error. `grep -n 'cancel:' agreementCopy.ts`
  // before typing, which is the rule for every key this task appends.
  addSectionLabel: "Add to section",
  newSectionLink: "+ New section",
  addBodyLabel: "New agreement",
  addBodyPlaceholder: "e.g. Screens off during family meals, no exceptions.",
  editTargetLabel: "Which agreement to edit",
  editBodyLabel: "New wording",
  removeTargetLabel: "Which agreement to remove",
  proposeNoteLabel: "Why (optional)",
  proposeSend: "Send for agreement",
  // "will be asked" agrees with any count, so this one needs no branch.
  proposeSubtitle: (names: string[]) =>
    `${joinNames(names)} will be asked to agree before it takes effect`,
  proposeNotePlaceholder: (names: string[]) => `Add a note for ${joinNames(names)}…`,
  // The design's warning verbatim but for the names. This is the one place
  // the verb has to agree with the count, because joinNames alone yields
  // "…Christine agree".
  removeWarning: (names: string[]) =>
    `This will be removed once ${joinNames(names)} ${names.length === 1 ? "agrees" : "agree"}. ` +
    "It stays in Version history, so you can always see it was there and restore it later.",
  // Rendered only when the seed pre-filled an add body. Version history's
  // Restore is the one entry point that does (decision 18).
  restoreNote: "Restoring is an ordinary proposal — it takes effect once everyone agrees.",
  proposeConflict:
    "This agreement changed while you were writing, so nothing was saved. " +
    "Close this and start again from the current wording.",
  proposeFallbackError: "Could not send that for agreement. Try again.",
  // The full body, never truncated: the design truncates but names no rule,
  // and inventing one risks two agreements sharing a label. The zero padding
  // is presentation, which is why the wire carries an integer (decision 11).
  targetOption: (number: number, body: string) => `${String(number).padStart(2, "0")} · ${body}`,
} as const;
