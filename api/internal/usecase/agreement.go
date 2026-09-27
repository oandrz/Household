package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// AgreementOwner is one owner as the Agreements screen names them: an id to
// compare against a viewer's own membership, a name to print.
type AgreementOwner struct{ MembershipID, Name string }

// AgreementLine is one live agreement as the screen prints it: its stored
// body and the display number computed across the whole document.
type AgreementLine struct {
	ID, Body string
	Number   int
}

// AgreementSectionView is one section with its live agreements. Count is
// len(Agreements) computed in the SAME walk that fills it (decision below),
// and Visible is stamped once rather than re-derived by every caller.
type AgreementSectionView struct {
	ID, Name   string
	Count      int
	Visible    bool
	Agreements []AgreementLine
}

// AgreementProposalView is one open (pending or parked) proposal, or the row
// a write just touched -- which may be accepted or withdrawn, statuses the
// document's own Proposals list excludes.
type AgreementProposalView struct {
	ID, Kind, Status, SectionID, SectionName, TargetAgreementID string
	Body, PreviousBody, Note, ParkNote                          string
	ProposedByMembershipID, ProposedByName                      string
	ProposedAt                                                  time.Time
	AwaitingNames                                               []string
	TargetChanged                                               bool
	// SignedByMembershipIDs never reaches the wire (agreementProposalDTO
	// omits it) -- carried unchanged so the HTTP layer can compute canAgree
	// from a membership id it already holds, not the browser comparing ids.
	SignedByMembershipIDs []string
}

// AgreementHistoryEntry is one accepted change, numbered with the version it
// produced -- v1 is the document before anything was agreed, so the first
// acceptance produces v2.
type AgreementHistoryEntry struct {
	Version                                  int
	ProposalID, Kind, SectionID, SectionName string
	Body, PreviousBody, Note, ProposedByName string
	SignedByNames                            []string
	AcceptedAt                               time.Time
}

// AgreementsView is the whole Agreements screen in one read.
type AgreementsView struct {
	Locked    bool
	Owners    []AgreementOwner // its length IS the owner count; no second count travels beside it
	Version   int
	UpdatedAt *time.Time
	Sections  []AgreementSectionView
	Proposals []AgreementProposalView // pending and parked only
	History   []AgreementHistoryEntry // newest first
}

// AgreementService composes the Agreements screen, its only write path.
// Two ports, no clock -- every write takes `at`, read once at the HTTP
// layer. No actor parameter: middleware enforces who's asking. No
// owner-count port either: the pending-card names already give the count
// as their length, so the locked screen and the pending card can't
// disagree.
type AgreementService struct {
	agreements AgreementRepository
	members    MembershipRepository
}

func NewAgreementService(agreements AgreementRepository, members MembershipRepository) *AgreementService {
	return &AgreementService{agreements: agreements, members: members}
}

// ErrAgreementDocumentCorrupt refuses to render a row this layer never
// wrote (an orphan section id, a misplaced proposal, a versionless history
// entry). Deliberately unmapped in MapDomainError -- a logged 500 beats a
// silently missing agreement. Declared here, not domain, for the same
// reason as member.go's ErrSessionRevocationFailed: service vocabulary,
// not a domain rule.
var ErrAgreementDocumentCorrupt = errors.New("agreement document holds a row this code never wrote")

// agreementLookups are what proposalView needs that AgreementsView doesn't
// carry. compose hands them back so a write's row runs through the same
// code the document's own proposals did -- one composition, two entry
// points, so a response's row and document can never disagree.
type agreementLookups struct {
	all          []domain.Membership
	names        map[string]string // membership id -> display name
	liveBody     map[string]string // live agreement id -> its current body
	sectionNames map[string]string // section id -> name
}

// Get composes the whole screen in one walk -- the only place any of these
// figures is derived. The version and the 01..N numbering are computed here
// and stored nowhere: a stored version could drift from the rows it counts,
// and stored numbers would need rewriting after every removal.
func (s *AgreementService) Get(ctx context.Context, householdID string) (AgreementsView, error) {
	view, _, err := s.compose(ctx, householdID)
	return view, err
}

// compose is Get plus the lookups. Get is this minus its second return value,
// so a write reading through compose IS reading back through Get: there is
// one walk over the document and the two entry points share it.
func (s *AgreementService) compose(ctx context.Context, householdID string) (AgreementsView, agreementLookups, error) {
	views, err := s.members.List(ctx, householdID)
	if err != nil {
		return AgreementsView{}, agreementLookups{}, err
	}
	doc, err := s.agreements.Document(ctx, householdID)
	if err != nil {
		return AgreementsView{}, agreementLookups{}, err
	}
	lk := agreementLookups{
		all:          membershipsFrom(views), // defined in member.go
		names:        make(map[string]string, len(views)),
		liveBody:     make(map[string]string, len(doc.Agreements)),
		sectionNames: make(map[string]string, len(doc.Sections)),
	}
	for _, v := range views {
		lk.names[v.Membership.ID] = v.User.DisplayName
	}
	owners := make([]AgreementOwner, 0, len(lk.all))
	for _, id := range domain.RequiredSigners(lk.all) {
		owners = append(owners, AgreementOwner{MembershipID: id, Name: lk.names[id]})
	}

	// Sections keep Document's order, each collecting its live agreements and
	// its Count in the SAME walk that fills them, so a total and its own
	// breakdown cannot diverge -- the BudgetService.Month defect.
	index := make(map[string]int, len(doc.Sections))
	for i, sec := range doc.Sections {
		index[sec.ID] = i
		lk.sectionNames[sec.ID] = sec.Name
	}
	grouped := make([][]AgreementRecord, len(doc.Sections))
	for _, a := range doc.Agreements {
		i, ok := index[a.SectionID]
		if !ok {
			return AgreementsView{}, agreementLookups{}, fmt.Errorf("%w: agreement %s names section %s",
				ErrAgreementDocumentCorrupt, a.ID, a.SectionID)
		}
		grouped[i] = append(grouped[i], a)
		lk.liveBody[a.ID] = a.Body
	}
	sizes := make([]int, len(grouped))
	for i, rows := range grouped {
		sizes[i] = len(rows)
	}
	numbers := domain.AgreementDisplayNumbers(sizes) // one call: numbering runs across sections
	sections := make([]AgreementSectionView, 0, len(doc.Sections))
	for i, sec := range doc.Sections {
		lines := make([]AgreementLine, 0, len(grouped[i]))
		for j, a := range grouped[i] {
			lines = append(lines, AgreementLine{ID: a.ID, Body: a.Body, Number: numbers[i][j]})
		}
		// Visible is stamped here so the screen reads a flag rather than
		// re-deriving it: the page renders the visible ones, the picker
		// offers them all, off one array.
		sections = append(sections, AgreementSectionView{ID: sec.ID, Name: sec.Name,
			Count: len(lines), Visible: len(lines) > 0, Agreements: lines})
	}

	proposals := make([]AgreementProposalView, 0, len(doc.Open))
	for _, p := range doc.Open {
		status, err := domain.ParseAgreementProposalStatus(p.Status)
		if err != nil {
			return AgreementsView{}, agreementLookups{}, err // a value no migration allowed: a corrupt row, not a request
		}
		if !status.IsOpen() {
			return AgreementsView{}, agreementLookups{}, fmt.Errorf("%w: proposal %s is %s in the open slice",
				ErrAgreementDocumentCorrupt, p.ID, p.Status)
		}
		view, err := s.proposalView(p, lk.all, lk.names, lk.liveBody, lk.sectionNames)
		if err != nil {
			return AgreementsView{}, agreementLookups{}, err
		}
		proposals = append(proposals, view)
	}

	// versionOf exists for exactly one consumer: each history entry's own
	// Version. No agreement carries an "added in v4" badge, because the design
	// draws none and a number nothing renders is a field that rots.
	versionOf := make(map[string]int, len(doc.Accepted))
	var updatedAt *time.Time
	for i, p := range doc.Accepted {
		status, err := domain.ParseAgreementProposalStatus(p.Status)
		if err != nil {
			return AgreementsView{}, agreementLookups{}, err
		}
		if status != domain.ProposalAccepted || p.ResolvedAt == nil {
			return AgreementsView{}, agreementLookups{}, fmt.Errorf("%w: proposal %s is %s in the accepted slice",
				ErrAgreementDocumentCorrupt, p.ID, p.Status)
		}
		versionOf[p.ID] = i + 2                // a document is v1 before anything is agreed
		updatedAt = doc.Accepted[i].ResolvedAt // resolved_at asc, so the last one wins
	}
	history := make([]AgreementHistoryEntry, 0, len(doc.Accepted))
	for i := len(doc.Accepted) - 1; i >= 0; i-- { // reversed, never re-sorted by a second rule
		p := doc.Accepted[i]
		version, ok := versionOf[p.ID]
		if !ok {
			return AgreementsView{}, agreementLookups{}, fmt.Errorf("%w: no version for proposal %s",
				ErrAgreementDocumentCorrupt, p.ID)
		}
		// Read with the comma-ok form, never map[key] straight into a field:
		// a miss would otherwise print another section's name under a change
		// nobody made there.
		sectionName, ok := lk.sectionNames[p.SectionID]
		if !ok {
			return AgreementsView{}, agreementLookups{}, fmt.Errorf("%w: accepted proposal %s names section %s",
				ErrAgreementDocumentCorrupt, p.ID, p.SectionID)
		}
		// A signer whose membership no longer resolves is OMITTED, never
		// joined as "" (never "Agreed by Andreas and "). The row survives
		// forever -- the membership id is a log column with no foreign key
		// -- so this is the ordinary case for a household a partner has left.
		signed := make([]string, 0, len(p.SignedByMembershipIDs))
		for _, id := range p.SignedByMembershipIDs {
			if name := lk.names[id]; name != "" {
				signed = append(signed, name)
			}
		}
		history = append(history, AgreementHistoryEntry{Version: version, ProposalID: p.ID,
			Kind: p.Kind, SectionID: p.SectionID, SectionName: sectionName,
			Body: p.Body, PreviousBody: p.PreviousBody, Note: p.Note,
			ProposedByName: lk.names[p.ProposedByMembershipID], SignedByNames: signed,
			AcceptedAt: *p.ResolvedAt})
	}

	// Locked gates writes, never hides the document -- a dropped-to-one-owner
	// household still sees what it agreed to. It travels beside the owners
	// list, so a screen never claims "locked" without an answered query.
	return AgreementsView{Locked: domain.AgreementsLocked(lk.all), Owners: owners,
		Version: len(doc.Accepted) + 1, UpdatedAt: updatedAt, Sections: sections,
		Proposals: proposals, History: history}, lk, nil
}

// proposalView composes one proposal, factored out of compose's walk
// because a write must return a row just accepted or withdrawn -- rows
// that walk excludes in SQL -- composed by the same code as the document.
func (s *AgreementService) proposalView(
	p AgreementProposalRecord,
	all []domain.Membership,
	names map[string]string,
	liveBody map[string]string,
	sectionNames map[string]string,
) (AgreementProposalView, error) {
	// Parsed here as well as in compose's open-slice walk: a write calls this
	// with a row that walk never saw, so each entry point fails closed on its
	// own rather than trusting the one before it.
	status, err := domain.ParseAgreementProposalStatus(p.Status)
	if err != nil {
		return AgreementProposalView{}, err
	}
	sectionName, ok := sectionNames[p.SectionID]
	if !ok {
		return AgreementProposalView{}, fmt.Errorf("%w: proposal %s names section %s",
			ErrAgreementDocumentCorrupt, p.ID, p.SectionID)
	}
	awaiting := domain.AwaitingSignature(all, p.SignedByMembershipIDs)
	awaitingNames := make([]string, 0, len(awaiting))
	for _, id := range awaiting {
		awaitingNames = append(awaitingNames, names[id])
	}
	// TargetChanged echoes the check Sign makes authoritative: whether the
	// target's live wording still equals PreviousBody. Asked ONLY of an open
	// proposal -- an accepted remove already took its target out of the live
	// set, so the literal rule would misfire "stale" on the very change that
	// just succeeded. A resolved proposal can't go stale (already answered);
	// an add has no target, so an empty id also can't go stale.
	targetChanged := false
	if status.IsOpen() && p.TargetAgreementID != "" {
		body, stillLive := liveBody[p.TargetAgreementID]
		targetChanged = !stillLive || body != p.PreviousBody
	}
	return AgreementProposalView{ID: p.ID, Kind: p.Kind, Status: p.Status,
		SectionID: p.SectionID, SectionName: sectionName, TargetAgreementID: p.TargetAgreementID,
		Body: p.Body, PreviousBody: p.PreviousBody, Note: p.Note, ParkNote: p.ParkNote,
		ProposedByMembershipID: p.ProposedByMembershipID,
		ProposedByName:         names[p.ProposedByMembershipID], ProposedAt: p.CreatedAt,
		AwaitingNames: awaitingNames, TargetChanged: targetChanged,
		SignedByMembershipIDs: p.SignedByMembershipIDs}, nil
}

// requireTwoOwners runs FIRST in every write: an agreement needs two
// owners to mean anything, so a one-owner household gets no drafts --
// uniform on purpose, since a rule some writes skip is one a reader gets
// wrong. An open proposal simply waits; there is no expiry and no decline.
func (s *AgreementService) requireTwoOwners(ctx context.Context, householdID string) error {
	views, err := s.members.List(ctx, householdID)
	if err != nil {
		return err
	}
	if domain.AgreementsLocked(membershipsFrom(views)) {
		return domain.ErrAgreementsNeedTwoOwners
	}
	return nil
}

// writtenProposal is the read-back every proposal write ends with: recompose the
// document and run the just-written row through the same proposalView code
// the document's own proposals use, so a response's row and document can
// never disagree -- including an accepted or withdrawn row, which the
// document's SQL walk excludes. The snapshot is taken AFTER the write and
// outside its transaction, so a concurrent agree may overtake it; the
// frontend's refetch stays authoritative. Landing a write that can't be
// read back is an error -- a 500, never a silent half-answer.
func (s *AgreementService) writtenProposal(ctx context.Context, householdID string,
	rec AgreementProposalRecord) (AgreementProposalView, AgreementsView, error) {
	doc, lk, err := s.compose(ctx, householdID)
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	view, err := s.proposalView(rec, lk.all, lk.names, lk.liveBody, lk.sectionNames)
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	return view, doc, nil
}

// CreateSection adds one label, never pre-checking the name -- the unique
// index decides the collision, avoiding a check-then-write race two owners
// could both pass. It returns the recomposed section, so the response and
// `agreements` can never disagree.
func (s *AgreementService) CreateSection(ctx context.Context, householdID, name string,
	at time.Time) (AgreementSectionView, AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementSectionView{}, AgreementsView{}, err
	}
	name = strings.TrimSpace(name) // trim first, so what is stored is what was validated
	if err := domain.ValidateAgreementSectionName(name); err != nil {
		return AgreementSectionView{}, AgreementsView{}, err
	}
	rec, err := s.agreements.CreateSection(ctx, householdID, name, at)
	if err != nil {
		return AgreementSectionView{}, AgreementsView{}, err
	}
	doc, _, err := s.compose(ctx, householdID)
	if err != nil {
		return AgreementSectionView{}, AgreementsView{}, err
	}
	for _, sec := range doc.Sections {
		if sec.ID == rec.ID {
			return sec, doc, nil
		}
	}
	return AgreementSectionView{}, AgreementsView{}, fmt.Errorf(
		"%w: section %s is missing from the document that just created it",
		ErrAgreementDocumentCorrupt, rec.ID)
}

// SeedStarterSections is "Use starter set": four labels, no agreements, so
// "everything here is here because you both agreed" stays true. Idempotent
// -- a second click is a no-op, not a 409. It answers with the document
// alone; the read-back only proves all four landed.
func (s *AgreementService) SeedStarterSections(ctx context.Context, householdID string,
	at time.Time) (AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementsView{}, err
	}
	if _, err := s.agreements.CreateSections(ctx, householdID, domain.StarterSectionNames(), at); err != nil {
		return AgreementsView{}, err
	}
	return s.Get(ctx, householdID)
}

// Propose stamps household and proposer from the route/session BEFORE
// validating, so a body naming another household is judged against its own
// constraints, not smuggled past them. No target check here -- it can only
// be atomic inside CreateProposal's transaction.
func (s *AgreementService) Propose(ctx context.Context, householdID, proposedByMembershipID string,
	p domain.AgreementProposal, at time.Time) (AgreementProposalView, AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	p.HouseholdID, p.ProposedByMembershipID = householdID, proposedByMembershipID
	p.Body, p.PreviousBody = strings.TrimSpace(p.Body), strings.TrimSpace(p.PreviousBody)
	p.Note = strings.TrimSpace(p.Note)
	if err := p.Validate(); err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	rec, err := s.agreements.CreateProposal(ctx, AgreementProposalWrite{HouseholdID: p.HouseholdID,
		Kind: p.Kind, SectionID: p.SectionID, TargetAgreementID: p.TargetAgreementID, Body: p.Body,
		PreviousBody: p.PreviousBody, Note: p.Note,
		ProposedByMembershipID: p.ProposedByMembershipID, CreatedAt: at})
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	return s.writtenProposal(ctx, householdID, rec)
}

// Sign validates nothing beyond the gate -- the owner count, current
// owners' signatures, and the target's wording are all decided inside the
// repository's transaction, the only place any of them is atomic.
func (s *AgreementService) Sign(ctx context.Context, householdID, proposalID, membershipID string,
	at time.Time) (AgreementProposalView, AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	rec, err := s.agreements.Sign(ctx, AgreementSignatureWrite{HouseholdID: householdID,
		ProposalID: proposalID, MembershipID: membershipID, At: at})
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	return s.writtenProposal(ctx, householdID, rec)
}

// Park is Discuss: the proposal stays open, shown on the Retros page's
// To-discuss block. The note is capped in RUNES, not bytes -- Chinese text
// would otherwise get a third of what the modal promised -- using its own
// constant, MaxAgreementParkNoteLen, independent of the proposal note's cap.
func (s *AgreementService) Park(ctx context.Context, householdID, proposalID, note string,
	at time.Time) (AgreementProposalView, AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	note = strings.TrimSpace(note) // trimmed before it is measured, as Validate's own contract says
	if utf8.RuneCountInString(note) > domain.MaxAgreementParkNoteLen {
		return AgreementProposalView{}, AgreementsView{}, domain.ErrAgreementParkNoteTooLong
	}
	rec, err := s.agreements.Park(ctx, householdID, proposalID, note, at)
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	return s.writtenProposal(ctx, householdID, rec)
}

// Withdraw does not branch on byMembershipID: only the HTTP layer knows
// who is asking -- the proposer until they stop being an owner, then any
// owner -- checked in the fixed 404 -> 403 -> 409 order (see Proposal). The
// id still travels for the repository's WHERE-clause backstop; a refusal
// from there is the store's answer, not this method's.
func (s *AgreementService) Withdraw(ctx context.Context, householdID, proposalID,
	byMembershipID string, at time.Time) (AgreementProposalView, AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	rec, err := s.agreements.Withdraw(ctx, householdID, proposalID, byMembershipID, at)
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	return s.writtenProposal(ctx, householdID, rec)
}

// Proposal is a read, and is deliberately NOT gated by requireTwoOwners: the
// withdraw handler needs 404 before 403 before 409, and a gate here would
// answer 409 for a proposal that does not exist. Pair it with Get's Owners
// list, which is what decides whether the proposer is still one.
func (s *AgreementService) Proposal(ctx context.Context, householdID,
	proposalID string) (AgreementProposalRecord, error) {
	return s.agreements.Proposal(ctx, householdID, proposalID)
}
