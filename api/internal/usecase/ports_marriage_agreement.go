// This file holds the marriage slice's agreement ports. ports.go lists
// every ports file.

package usecase

import (
	"context"
	"time"
)

// AgreementSectionRecord is one stored section: a label, not a promise --
// creating one is immediate and unsigned.
type AgreementSectionRecord struct {
	ID, Name  string
	CreatedAt time.Time
}

// AgreementRecord is one LIVE agreement. A removed one never appears here:
// removal is a stamp, not a delete, and removed_at IS NULL belongs in SQL,
// never in a caller.
type AgreementRecord struct {
	ID, SectionID, Body, AddedByProposalID string
	CreatedAt                              time.Time
}

// AgreementProposalRecord is one proposed change as stored. Kind and Status
// hold the column re-parsed, never cast. ResolvedAt is a pointer because
// "still open" is a state and the zero time is not a moment.
// SignedByMembershipIDs may hold a signature from someone who is no longer an
// owner: a true record that stops counting.
type AgreementProposalRecord struct {
	ID, Kind, Status, SectionID, TargetAgreementID, Body, PreviousBody, Note, ParkNote,
	ProposedByMembershipID string
	CreatedAt             time.Time
	ResolvedAt            *time.Time
	SignedByMembershipIDs []string
}

// AgreementDocument is the whole screen in one read. Open and Accepted are
// two slices because they are in two orders and one cannot hold both;
// withdrawn proposals appear in neither, excluded in SQL. Every slice
// non-nil, so a caller ranges over it without a check.
type AgreementDocument struct {
	Sections   []AgreementSectionRecord
	Agreements []AgreementRecord         // live only, removed_at IS NULL
	Open       []AgreementProposalRecord // pending and parked, created_at asc
	Accepted   []AgreementProposalRecord // accepted only, resolved_at asc
}

// AgreementProposalWrite is one propose. HouseholdID and
// ProposedByMembershipID are stamped by the service from the route and the
// session, never read from a request body. SectionID is set only for an add:
// on an edit or a remove the repository copies the target's own section, so
// there is nothing here for a caller to get wrong.
type AgreementProposalWrite struct {
	HouseholdID, Kind, SectionID, TargetAgreementID, Body, PreviousBody, Note,
	ProposedByMembershipID string
	CreatedAt time.Time
}

// AgreementSignatureWrite is one Agree. MembershipID is STORED, never
// consulted for permission -- PaymentWrite.PaidByMembershipID's contract.
type AgreementSignatureWrite struct {
	HouseholdID, ProposalID, MembershipID string
	At                                    time.Time
}

// AgreementRepository stores one household's agreements, the sections that
// group them, and the append-only log of every change ever proposed. Every
// method filters on householdID in SQL: another household's row must be
// indistinguishable from one that does not exist. Ordering is created_at, id
// everywhere except AgreementDocument.Accepted, which is resolved_at, id --
// a version is the k-th ACCEPTANCE, and two proposals created A then B can be
// accepted B then A. Nothing here reads a clock.
type AgreementRepository interface {
	// Document is the whole screen in one read, kind and status re-parsed and
	// never cast. Unbounded on purpose: a household writes a few agreements a
	// year, and the version, the history list and the Retros page's
	// To-discuss block all derive from more than one of its four slices.
	Document(ctx context.Context, householdID string) (AgreementDocument, error)
	// Proposal returns one proposal whatever its status, withdrawn included,
	// so the withdraw handler's proposer check costs one query rather than a
	// composed document. domain.ErrNotFound for an unknown or foreign id.
	Proposal(ctx context.Context, householdID, proposalID string) (AgreementProposalRecord, error)
	// CreateSection adds one label. A clash with UNIQUE (household_id, name)
	// is domain.ErrAgreementSectionNameTaken, mapped by CONSTRAINT NAME above
	// the generic 23505 case, or the screen cannot tell "you already have
	// that section" from anything else that could collide.
	CreateSection(ctx context.Context, householdID, name string, createdAt time.Time) (AgreementSectionRecord, error)
	// CreateSections is "Use starter set": every name in ONE transaction, ON
	// CONFLICT DO NOTHING so a second click is a no-op rather than a 409, then
	// read back inside it. Two of four landing would leave a household
	// half-seeded with no button left to ask for the rest. The read-back is how
	// the caller proves all four landed; nothing renders from its order, since
	// the route answers with the whole freshly composed document and render
	// order is always the document's.
	CreateSections(ctx context.Context, householdID string, names []string, createdAt time.Time) ([]AgreementSectionRecord, error)
	// CreateProposal writes the proposal row and the proposer's implicit
	// signature, verifying the target first for an edit or a remove, all in ONE
	// transaction: all of it happens or none does. A proposal without its
	// proposer's signature would ask both owners to be the second signer of a
	// set of one -- unrepairable, the same failure InviteRepository.Accept's own
	// comment describes. The target must exist in this household, still be live,
	// and its body must equal PreviousBody exactly, compared as stored and never
	// re-trimmed; either failure is domain.ErrAgreementChanged with nothing
	// written. The target's section_id is copied onto the proposal in the same
	// statement, which is why domain.AgreementProposal.Validate refuses a
	// caller-supplied one on an edit or a remove. Zero rows on the proposer
	// lookup through memberships (this household, role = 'owner') rolls it all
	// back with domain.ErrForbidden.
	CreateProposal(ctx context.Context, in AgreementProposalWrite) (AgreementProposalRecord, error)
	// Sign records one Agree and, when that completes the signing set, applies
	// the change -- all in ONE transaction, on that transaction's OWN
	// connection: either all of it happens or none of it does. A pool-backed
	// call inside pgx.BeginFunc takes a second connection while the first is
	// held, and enough concurrent signers deadlock against MaxConns, the way
	// VisionRepo.Save once did. It is the only method that writes an agreements
	// row. The set is every CURRENT owner, counted in this transaction, and the
	// lock that matters is on the TARGET agreement, not the proposal; that
	// target check runs BEFORE the signature lands, or a middle signer's
	// agreement is recorded against wording that has already moved. Applying is
	// a switch on kind with a refusing default: an add inserts, a remove stamps,
	// an edit does both -- so an edit CHANGES the agreement's id, and the new
	// row sorts last in its section, exactly as created_at, id puts it.
	// domain.ErrNotFound for an unknown id, domain.ErrAgreementNotOpen for a
	// resolved one, domain.ErrAgreementChanged when the target moved,
	// domain.ErrForbidden when the signer is not an owner here.
	Sign(ctx context.Context, in AgreementSignatureWrite) (AgreementProposalRecord, error)
	// Park is Discuss: the proposal stays OPEN and moves to the Retros page's
	// To-discuss block; parking twice replaces the note. One guarded UPDATE with
	// the status condition in the WHERE clause, never a service if, because a
	// check-then-write races; zero rows is diagnosed by one re-read -- gone is
	// domain.ErrNotFound, resolved is domain.ErrAgreementNotOpen, the re-read's
	// own failure passes through untouched. NOTHING here touches a retro table
	// and there is no foreign key to a retro row: the next retro usually does
	// not exist yet, which is exactly when a couple parks something.
	Park(ctx context.Context, householdID, proposalID, note string, at time.Time) (AgreementProposalRecord, error)
	// Withdraw is the same guarded UPDATE plus AND (proposed_by_membership_id
	// = $by OR that membership is no longer an owner here): proposer-only
	// until the proposer leaves, then any owner. That clause is a BACKSTOP
	// -- the handler decides and answers first, and this method never
	// branches on $by. Four diagnose legs, in order: gone is
	// domain.ErrNotFound; resolved is domain.ErrAgreementNotOpen; open,
	// someone else's, and that someone still an owner is domain.ErrForbidden;
	// the re-read's own failure is folded into none of the other three.
	Withdraw(ctx context.Context, householdID, proposalID, byMembershipID string, at time.Time) (AgreementProposalRecord, error)
}
