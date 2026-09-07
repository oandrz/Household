package httpadapter

import (
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// maxAgreementRequestBodyBytes replaces the ordinary maxRequestBodyBytes for
// two routes only: POST /marriage/agreements/proposals and
// POST /marriage/agreements/proposals/{id}/park. Between them they carry four
// rune-capped free-text fields (body, previousBody, note, park note) at 500
// runes each, and a 500-rune CJK field alone is 1500 bytes -- the 1 KiB
// default would answer 413 to a body the domain considers legal. 8 KiB clears
// all four comfortably while still refusing anything absurd, the same
// reasoning maxRetroRequestBodyBytes and maxVisionRequestBodyBytes give for
// their own overrides.
const maxAgreementRequestBodyBytes = 8 * 1024

type createAgreementSectionRequest struct {
	Name string `json:"name"`
}

// SectionID is an add's alone. The handler blanks it for any other kind
// rather than refusing: the propose modal still holds one from add mode, and
// on an edit or a remove the server takes the section from the target anyway.
// PreviousBody is required rather than optional on those two kinds -- an
// agreement body is never empty, so an omitted one would always read as stale.
type proposeAgreementChangeRequest struct {
	Kind              string `json:"kind"`              // "add" | "edit" | "remove", parsed here (decision 21)
	SectionID         string `json:"sectionId"`         // add: required. edit/remove: blanked by the handler
	TargetAgreementID string `json:"targetAgreementId"` // edit/remove: required
	Body              string `json:"body"`              // add/edit: required
	PreviousBody      string `json:"previousBody"`      // edit/remove: required
	Note              string `json:"note"`              // always optional
}

// Note may be "", but the body is always sent: an absent one is 400
// INVALID_BODY. Discuss is a bare button, so an empty park note is ordinary.
type parkAgreementProposalRequest struct {
	Note string `json:"note"`
}

// Number is the design's "01" as an integer, derived at render (decision 11);
// the zero padding is the browser's. No addedAt and no signer ids on the wire:
// the design renders neither, and a field nothing reads is a field nothing
// keeps honest.
type agreementDTO struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Body   string `json:"body"`
}

// Every section travels, empty ones included (decision 8). Count is its live
// agreements and Visible is Count > 0, both stamped by the service: the page
// renders the visible ones and the propose picker offers them all, off ONE
// array. Two arrays would ship every section twice on every write response,
// for one boolean.
type agreementSectionDTO struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Count      int            `json:"count"`
	Visible    bool           `json:"visible"`
	Agreements []agreementDTO `json:"agreements"`
}

type agreementOwnerDTO struct {
	MembershipID string `json:"membershipId"`
	Name         string `json:"name"`
}

type agreementProposalDTO struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // "add" | "edit" | "remove"
	// "pending" | "parked" in the document; a WRITE response may also carry
	// "accepted" or "withdrawn", which is why the frontend's enum is
	// four-valued and the card's own switch is what narrows it.
	Status                 string    `json:"status"`
	SectionID              string    `json:"sectionId"` // for edit and remove it is the target's section
	SectionName            string    `json:"sectionName"`
	TargetAgreementID      string    `json:"targetAgreementId"` // "" for add
	Body                   string    `json:"body"`              // "" for remove
	PreviousBody           string    `json:"previousBody"`      // "" for add
	Note                   string    `json:"note"`
	ParkNote               string    `json:"parkNote"` // may be "" even when parked: Discuss is a bare button
	ProposedByMembershipID string    `json:"proposedByMembershipId"`
	ProposedByName         string    `json:"proposedByName"` // "" if the membership no longer resolves
	ProposedAt             time.Time `json:"proposedAt"`
	AwaitingNames          []string  `json:"awaitingNames"` // owners yet to sign, evaluated live (decision 4)
	TargetChanged          bool      `json:"targetChanged"` // previousBody no longer matches the live target
	CanAgree               bool      `json:"canAgree"`
	CanWithdraw            bool      `json:"canWithdraw"`
}

// Version is the one this change PRODUCED; SignedByNames is read from the
// signatures it collected, never from today's owners; and there is no display
// number, because a change accepted two years ago has no position in today's
// document.
type agreementHistoryEntryDTO struct {
	Version        int       `json:"version"`
	ProposalID     string    `json:"proposalId"`
	Kind           string    `json:"kind"`
	SectionID      string    `json:"sectionId"`
	SectionName    string    `json:"sectionName"`
	Body           string    `json:"body"`
	PreviousBody   string    `json:"previousBody"`
	Note           string    `json:"note"`
	ProposedByName string    `json:"proposedByName"`
	SignedByNames  []string  `json:"signedByNames"`
	AcceptedAt     time.Time `json:"acceptedAt"`
}

type agreementsDocumentDTO struct {
	Locked bool `json:"locked"`
	// Its length IS the owner count; no second count travels beside these rows,
	// so the locked screen and the pending card cannot disagree about it.
	Owners    []agreementOwnerDTO        `json:"owners"`
	Version   int                        `json:"version"`   // count(accepted) + 1 (decision 10)
	UpdatedAt *time.Time                 `json:"updatedAt"` // null until the first accepted change
	Sections  []agreementSectionDTO      `json:"sections"`  // every slice on the wire is [], never null
	Proposals []agreementProposalDTO     `json:"proposals"` // pending and parked only
	History   []agreementHistoryEntryDTO `json:"history"`   // every accepted change, newest first
}

type agreementsResponse struct {
	Agreements agreementsDocumentDTO `json:"agreements"`
}

type agreementSectionWriteResponse struct {
	Section    agreementSectionDTO   `json:"section"`
	Agreements agreementsDocumentDTO `json:"agreements"`
}

// Proposal is the row the write touched at the status it now holds, and the
// only place "accepted" or "withdrawn" reaches the wire. No screen renders it
// -- every card re-renders off Agreements -- and it exists so the HTTP tests
// can assert the transition field by field, which a whole-document assertion
// would bury.
type agreementProposalWriteResponse struct {
	Proposal   agreementProposalDTO  `json:"proposal"`
	Agreements agreementsDocumentDTO `json:"agreements"`
}

// toAgreementProposalDTO stamps canAgree and canWithdraw HERE, not in the
// service: the service composes what is true of the household, and only the
// HTTP layer knows who is asking (CLAUDE.md -- no service takes an actor
// parameter to decide whether a caller may act).
//
// canAgree's second clause is decision 16: because the signer set is evaluated
// live, a proposal can become fully signed by nobody's action -- three owners,
// one proposes, one agrees, the third leaves -- and completion is only ever
// decided during a signing, so without this clause that proposal would sit
// pending, needing nobody, forever. The idempotent re-Agree is what closes it.
//
// canWithdraw tests the proposer against the LIVE owner set (decision 15),
// never a column: once the proposer is no longer an owner, any owner may
// withdraw it, or a proposal left behind by a departed partner could never be
// removed by anyone.
//
// Both say the caller MAY act, not that the write will succeed -- freshness is
// targetChanged's job -- and both are false on a locked household, so nothing
// clickable would 409 (decision 3).
func toAgreementProposalDTO(p usecase.AgreementProposalView, doc usecase.AgreementsView, viewer string) agreementProposalDTO {
	awaiting := make([]string, 0, len(p.AwaitingNames))
	awaiting = append(awaiting, p.AwaitingNames...)
	proposerIsOwner := slices.ContainsFunc(doc.Owners, func(o usecase.AgreementOwner) bool {
		return o.MembershipID == p.ProposedByMembershipID
	})
	return agreementProposalDTO{
		ID:                     p.ID,
		Kind:                   p.Kind,
		Status:                 p.Status,
		SectionID:              p.SectionID,
		SectionName:            p.SectionName,
		TargetAgreementID:      p.TargetAgreementID,
		Body:                   p.Body,
		PreviousBody:           p.PreviousBody,
		Note:                   p.Note,
		ParkNote:               p.ParkNote,
		ProposedByMembershipID: p.ProposedByMembershipID,
		ProposedByName:         p.ProposedByName,
		ProposedAt:             p.ProposedAt,
		AwaitingNames:          awaiting,
		TargetChanged:          p.TargetChanged,
		CanAgree:               !doc.Locked && (!slices.Contains(p.SignedByMembershipIDs, viewer) || len(awaiting) == 0),
		CanWithdraw:            !doc.Locked && (p.ProposedByMembershipID == viewer || !proposerIsOwner),
	}
}

func toAgreementSectionDTO(s usecase.AgreementSectionView) agreementSectionDTO {
	items := make([]agreementDTO, 0, len(s.Agreements))
	for _, a := range s.Agreements {
		items = append(items, agreementDTO{ID: a.ID, Number: a.Number, Body: a.Body})
	}
	return agreementSectionDTO{
		ID: s.ID, Name: s.Name, Count: s.Count, Visible: s.Visible, Agreements: items,
	}
}

func toAgreementsDTO(doc usecase.AgreementsView, viewer string) agreementsDocumentDTO {
	owners := make([]agreementOwnerDTO, 0, len(doc.Owners))
	for _, o := range doc.Owners {
		owners = append(owners, agreementOwnerDTO{MembershipID: o.MembershipID, Name: o.Name})
	}
	sections := make([]agreementSectionDTO, 0, len(doc.Sections))
	for _, s := range doc.Sections {
		sections = append(sections, toAgreementSectionDTO(s))
	}
	proposals := make([]agreementProposalDTO, 0, len(doc.Proposals))
	for _, p := range doc.Proposals {
		proposals = append(proposals, toAgreementProposalDTO(p, doc, viewer))
	}
	history := make([]agreementHistoryEntryDTO, 0, len(doc.History))
	for _, h := range doc.History {
		names := make([]string, 0, len(h.SignedByNames))
		names = append(names, h.SignedByNames...)
		history = append(history, agreementHistoryEntryDTO{
			Version:        h.Version,
			ProposalID:     h.ProposalID,
			Kind:           h.Kind,
			SectionID:      h.SectionID,
			SectionName:    h.SectionName,
			Body:           h.Body,
			PreviousBody:   h.PreviousBody,
			Note:           h.Note,
			ProposedByName: h.ProposedByName,
			SignedByNames:  names,
			AcceptedAt:     h.AcceptedAt,
		})
	}
	return agreementsDocumentDTO{
		Locked:    doc.Locked,
		Owners:    owners,
		Version:   doc.Version,
		UpdatedAt: doc.UpdatedAt,
		Sections:  sections,
		Proposals: proposals,
		History:   history,
	}
}

// handleGetAgreements answers 200 for a locked household too (decision 3):
// what it lacks is a second owner, not permission, and the empty state IS the
// page. The read is never gated on locked -- a household that dropped to one
// owner keeps seeing what it agreed to and its frozen proposals, and the
// banner explaining why is the frontend's job off doc.locked.
func handleGetAgreements(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		doc, err := deps.Agreements.Get(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, agreementsResponse{
			Agreements: toAgreementsDTO(doc, scope.Membership.ID),
		})
	}
}

// respondProposal answers the body every proposal write shares: the row the
// write touched at the status it now holds, plus the whole freshly composed
// document, because each write moves the version, the 01..N numbering, the
// history list and which proposals are open.
//
// The document is composed AFTER the write and outside its transaction, so a
// concurrent agree may already have overtaken it. That is accepted rather than
// worked around: the frontend's refetch stays the authority, and a response
// that tried to be authoritative would need the read inside the write's
// transaction for no gain the screen can see.
func respondProposal(w http.ResponseWriter, viewer string, status int,
	p usecase.AgreementProposalView, doc usecase.AgreementsView) {
	WriteJSON(w, status, agreementProposalWriteResponse{
		Proposal:   toAgreementProposalDTO(p, doc, viewer),
		Agreements: toAgreementsDTO(doc, viewer),
	})
}

// handleProposeAgreementChange parses the kind itself and answers 422 from
// here (decision 21), the way parseVisionYear answers a bad year. A kind
// arrives from two places -- a request body, where a bad value is the caller's
// mistake, and a database column, where a bad value is a corrupt row -- and one
// sentinel serving both jobs would make a broken row indistinguishable from a
// typo. So domain.ErrUnknownAgreementProposalKind deliberately has no
// MapDomainError case: anything reaching the mapper with it came from a column.
//
// SectionID is blanked for anything but an add, because the modal still holds
// one from add mode and on an edit or a remove the server copies the section
// from the target anyway. Validate's refusal of a caller-supplied section on
// those two kinds stays the fail-closed backstop behind that.
func handleProposeAgreementChange(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req proposeAgreementChangeRequest
		if !decodeJSONBodyLimit(w, r, &req, maxAgreementRequestBodyBytes) {
			return
		}
		kind, err := domain.ParseAgreementProposalKind(req.Kind)
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "AGREEMENT_KIND_INVALID",
				"That is not a change we can propose.", nil)
			return
		}
		sectionID := req.SectionID
		if kind != domain.ProposalAdd {
			sectionID = ""
		}
		// HouseholdID and ProposedByMembershipID are left zero in the struct on
		// purpose: the service stamps both from the two arguments below -- the
		// route and the session -- so a body carrying either is ignored rather
		// than trusted. Filling them in here from req would be the mistake.
		p, doc, err := deps.Agreements.Propose(r.Context(), scope.HouseholdID, scope.Membership.ID,
			domain.AgreementProposal{
				Kind:              string(kind),
				SectionID:         sectionID,
				TargetAgreementID: req.TargetAgreementID,
				Body:              req.Body,
				PreviousBody:      req.PreviousBody,
				Note:              req.Note,
			}, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		respondProposal(w, scope.Membership.ID, http.StatusCreated, p, doc)
	}
}

// handleWithdrawAgreementProposal owns the refusal order 404 -> 403 -> 409
// (decision 22). It must read the proposal before it can know whose it is, so
// the order is a property of THIS handler and not of the guards, and without
// it "every write refuses 409 when the household is locked" is false on the
// wire.
//
// The proposer is compared against the LIVE owner set, never a column: once
// they are no longer an owner, any owner may withdraw it (decision 15), or a
// proposal left behind by a departed partner could never be removed by anyone
// -- the permanently-stuck state this codebase already shipped once, in
// invites. That is a "who is asking" question, which is why it lives here and
// no service takes an actor parameter for it. AgreementRepository.Withdraw's
// own SQL clause is the backstop behind this, and it never branches on $by.
func handleWithdrawAgreementProposal(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		proposalID := chi.URLParam(r, "id")

		record, err := deps.Agreements.Proposal(r.Context(), scope.HouseholdID, proposalID)
		if err != nil {
			MapDomainError(w, r, err) // an unknown or foreign id is ErrNotFound -> 404
			return
		}
		members, err := deps.Memberships.List(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		all := make([]domain.Membership, 0, len(members))
		for _, m := range members {
			all = append(all, m.Membership)
		}
		if record.ProposedByMembershipID != scope.Membership.ID &&
			slices.Contains(domain.RequiredSigners(all), record.ProposedByMembershipID) {
			WriteError(w, http.StatusForbidden, "AGREEMENT_NOT_PROPOSER",
				"Only the owner who proposed this can withdraw it.", nil)
			return
		}

		p, doc, err := deps.Agreements.Withdraw(r.Context(), scope.HouseholdID,
			proposalID, scope.Membership.ID, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err) // a locked household refuses here, 409, and only here
			return
		}
		respondProposal(w, scope.Membership.ID, http.StatusOK, p, doc)
	}
}

// handleCreateAgreementSection answers 201, because a section creates a row --
// and creating one is immediate and unsigned, since a heading is not a promise
// (decision 8). decodeJSONBody's 1 KiB default is right here: the body is one
// name, capped at 60 runes.
func handleCreateAgreementSection(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req createAgreementSectionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		section, doc, err := deps.Agreements.CreateSection(r.Context(), scope.HouseholdID,
			req.Name, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusCreated, agreementSectionWriteResponse{
			Section:    toAgreementSectionDTO(section),
			Agreements: toAgreementsDTO(doc, scope.Membership.ID),
		})
	}
}

// handleSeedStarterAgreementSections answers 200, not 201: the starter set is
// idempotent (decision 17) and a second click may create nothing. It answers
// the bare document rather than a row plus a document because nothing renders
// from the four rows' order -- render order is always the document's.
func handleSeedStarterAgreementSections(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		doc, err := deps.Agreements.SeedStarterSections(r.Context(), scope.HouseholdID, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, agreementsResponse{
			Agreements: toAgreementsDTO(doc, scope.Membership.ID),
		})
	}
}

// handleAgreeAgreementProposal records one Agree. A repeat Agree is an
// idempotent 200 that may complete the set (decision 16) -- the signature write
// is an upsert -- while an Agree on a proposal already accepted or withdrawn is
// 409 AGREEMENT_PROPOSAL_RESOLVED from the service. Every count and comparison
// that decides the outcome lives inside the repository's transaction; nothing
// is decided here.
func handleAgreeAgreementProposal(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		p, doc, err := deps.Agreements.Sign(r.Context(), scope.HouseholdID,
			chi.URLParam(r, "id"), scope.Membership.ID, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		respondProposal(w, scope.Membership.ID, http.StatusOK, p, doc)
	}
}

// handleParkAgreementProposal is Discuss: the proposal stays open, stays
// answerable, and is rendered in the read-only To-discuss block on the Retros
// page (decision 7). Nothing here touches a retro table and there is no
// foreign key to a retro row -- the next retro usually does not exist yet,
// which is exactly when a couple parks something.
func handleParkAgreementProposal(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req parkAgreementProposalRequest
		if !decodeJSONBodyLimit(w, r, &req, maxAgreementRequestBodyBytes) {
			return
		}
		p, doc, err := deps.Agreements.Park(r.Context(), scope.HouseholdID,
			chi.URLParam(r, "id"), req.Note, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		respondProposal(w, scope.Membership.ID, http.StatusOK, p, doc)
	}
}
