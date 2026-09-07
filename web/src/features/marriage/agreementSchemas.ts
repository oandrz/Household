// Zod mirrors of the DTOs in api/internal/adapter/http/agreement_handlers.go
// (agreementDTO, agreementSectionDTO, agreementOwnerDTO, agreementProposalDTO,
// agreementHistoryEntryDTO, agreementsDocumentDTO and the three response
// envelopes). These follow the Go structs field for field rather than the
// design doc, the convention visionSchemas.ts:1-5 and retroSchemas.ts already
// use -- the Go comments are what say which fields can be meaningless and why.
import { z } from "zod";

// Mirrors domain.AgreementProposalKind. Three values and no default: a fourth
// kind needs a migration as well as a case, and anything this schema does not
// recognise must fail the parse rather than silently render as an "add".
export const agreementKindSchema = z.enum(["add", "edit", "remove"]);
export type AgreementKind = z.infer<typeof agreementKindSchema>;

// FOUR-valued, deliberately. The document only ever carries pending and
// parked (accepted and withdrawn are excluded in SQL), but a WRITE response
// carries the row at the status it now holds -- an Agree that completes a
// signing set answers "accepted", a withdraw answers "withdrawn". apiFetch
// turns a failed parse of an ok response into a thrown error, so a two-valued
// enum here would fail the very click that finished the agreement. Narrowing
// to pending/parked is ProposalCard's own switch (Task 12), never the
// schema's job.
export const agreementStatusSchema = z.enum(["pending", "parked", "accepted", "withdrawn"]);

// number is the design's "01" as an integer, composed by the service on every
// read and stored nowhere (spec decisions 10 and 11) -- the zero padding is
// the browser's, so this is a plain int and never a pre-padded string.
export const agreementSchema = z.object({
  id: z.string(),
  number: z.number().int(),
  body: z.string(),
});
export type Agreement = z.infer<typeof agreementSchema>;

// Every section travels, empty ones included: the page renders the visible
// ones and the Propose picker offers them all, off ONE array. count is its
// live agreements and visible is count > 0, both stamped by the service --
// the screen reads flags here, it does not re-derive the rule.
export const agreementSectionSchema = z.object({
  id: z.string(),
  name: z.string(),
  count: z.number().int(),
  visible: z.boolean(),
  agreements: z.array(agreementSchema),
});
export type AgreementSection = z.infer<typeof agreementSectionSchema>;

export const agreementOwnerSchema = z.object({
  membershipId: z.string(),
  name: z.string(),
});
export type AgreementOwner = z.infer<typeof agreementOwnerSchema>;

// proposedByName is "" when the membership no longer resolves -- an ordinary
// state for any household a partner has left, not a corruption, since the
// signature and proposal rows outlive the membership by design. The card
// drops attribution rather than printing an empty name.
//
// canAgree/canWithdraw say the caller MAY act, not that the write will
// succeed; freshness is targetChanged's job. All four are server-stamped, and
// nothing in the browser recomputes them from a membership id.
export const agreementProposalSchema = z.object({
  id: z.string(),
  kind: agreementKindSchema,
  status: agreementStatusSchema,
  sectionId: z.string(),
  sectionName: z.string(),
  targetAgreementId: z.string(),
  body: z.string(),
  previousBody: z.string(),
  note: z.string(),
  parkNote: z.string(),
  proposedByMembershipId: z.string(),
  proposedByName: z.string(),
  proposedAt: z.string(),
  awaitingNames: z.array(z.string()),
  targetChanged: z.boolean(),
  canAgree: z.boolean(),
  canWithdraw: z.boolean(),
});
export type AgreementProposal = z.infer<typeof agreementProposalSchema>;

// One accepted change. version is the one this change PRODUCED, and
// signedByNames is read from the signatures it collected, never from today's
// owners -- a signer whose membership no longer resolves is omitted from the
// list rather than joined as an empty string.
export const agreementHistoryEntrySchema = z.object({
  version: z.number().int(),
  proposalId: z.string(),
  kind: agreementKindSchema,
  sectionId: z.string(),
  sectionName: z.string(),
  body: z.string(),
  previousBody: z.string(),
  note: z.string(),
  proposedByName: z.string(),
  signedByNames: z.array(z.string()),
  acceptedAt: z.string(),
});
export type AgreementHistoryEntry = z.infer<typeof agreementHistoryEntrySchema>;

// Timestamps are z.string(): the wire carries RFC 3339 and only the three
// label helpers in agreementCopy.ts parse them, so a Date on the boundary
// would be a second representation nothing needs.
//
// updatedAt is null until something has been agreed -- rendered as an absent
// clause, never as "v1, updated —". Every array is [] on the wire and never
// null (the service builds each with make(..., 0, n)), so these are required
// arrays rather than optional ones with a default: a missing key means the
// server drifted, and that must fail the parse.
export const agreementsDocumentSchema = z.object({
  locked: z.boolean(),
  owners: z.array(agreementOwnerSchema),
  version: z.number().int(),
  updatedAt: z.string().nullable(),
  sections: z.array(agreementSectionSchema),
  proposals: z.array(agreementProposalSchema),
  history: z.array(agreementHistoryEntrySchema),
});
export type AgreementsDocument = z.infer<typeof agreementsDocumentSchema>;

// The three envelopes. Every 2xx on this feature carries a JSON body and every
// body is wrapped -- the read is { agreements }, a section write is
// { section, agreements } and the four proposal writes are
// { proposal, agreements }. The hook parses the envelope and hands its callers
// the inner object.
export const agreementsResponseSchema = z.object({ agreements: agreementsDocumentSchema });
export const agreementSectionWriteResponseSchema = z.object({
  section: agreementSectionSchema,
  agreements: agreementsDocumentSchema,
});
export const agreementProposalWriteResponseSchema = z.object({
  proposal: agreementProposalSchema,
  agreements: agreementsDocumentSchema,
});
