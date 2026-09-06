package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	// SignedByMembershipIDs never reaches the wire (agreementProposalDTO has
	// no such field). It carries the record's slice unchanged so the HTTP
	// layer can compute canAgree = !locked && (!signedByViewer ||
	// len(awaitingNames) == 0) from a membership id it already holds, rather
	// than the browser comparing membership ids itself.
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

// AgreementService composes the Agreements screen and is its only write path.
// Two ports and no clock: every write takes at time.Time, so the wall clock is
// read once, at the HTTP layer. No method takes an actor parameter to decide
// whether a caller may act -- middleware enforces who is asking. There is
// deliberately no owner-count port: every pending card names who has still to
// sign, so the names are needed anyway and the count is that list's length --
// the locked screen and the pending card cannot disagree.
type AgreementService struct {
	agreements AgreementRepository
	members    MembershipRepository
}

func NewAgreementService(agreements AgreementRepository, members MembershipRepository) *AgreementService {
	return &AgreementService{agreements: agreements, members: members}
}

// ErrAgreementDocumentCorrupt is this layer refusing to render a document
// holding a row it never wrote: a section id naming no section, a proposal in
// the wrong slice, a history entry with no version. Deliberately unmapped in
// MapDomainError -- a logged 500 beats an agreement silently missing from the
// page or a history entry numbered v0. Declared here for the reason
// ErrSessionRevocationFailed is declared in member.go: it is this service's
// own vocabulary, not a domain rule.
var ErrAgreementDocumentCorrupt = errors.New("agreement document holds a row this code never wrote")

// agreementLookups are the four things proposalView needs and AgreementsView
// does not carry. compose hands them back so a write can run the row it just
// touched through exactly the code the document's own proposals went through:
// one composition, two entry points, and no way for the row and the document
// in a single response to describe the household differently.
type agreementLookups struct {
	all          []domain.Membership
	names        map[string]string // membership id -> display name
	liveBody     map[string]string // live agreement id -> its current body
	sectionNames map[string]string // section id -> name
}

// Get composes the whole screen in one walk -- the only place any of these
// figures is derived. The version and the 01..N numbering are computed here
// and stored nowhere (decisions 10 and 11).
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
		all:          membershipsFrom(views), // member.go:130
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
		// re-deriving decision 8: the page renders the visible ones, the
		// picker offers them all, off one array.
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
		// joined as "", or history renders "Agreed by Andreas and ". Decision
		// 20 keeps the row forever, so this is the ordinary case for a
		// household a partner has left.
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

	// Locked gates writes and never hides the document (decision 3), and it
	// travels beside the owners list on every response -- a screen may only
	// say a household is locked once an answered query has said so.
	return AgreementsView{Locked: domain.AgreementsLocked(lk.all), Owners: owners,
		Version: len(doc.Accepted) + 1, UpdatedAt: updatedAt, Sections: sections,
		Proposals: proposals, History: history}, lk, nil
}

// proposalView composes one proposal. It is factored out of compose's walk
// over doc.Open because a write must return a row that has just become
// accepted or withdrawn -- rows that walk excludes in SQL -- and the row and
// the document in one response have to be composed by the same code.
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
	// TargetChanged is the read-side echo of decision 13, whose authority
	// stays in Sign. It is asked ONLY of an open proposal: an accepted remove
	// has taken its own target out of the live set, so the literal rule would
	// answer true on the very change that just succeeded, and the write
	// response would read as "your agree was stale". A resolved proposal
	// cannot go stale -- the question it asked has been answered. An add has
	// no target, so an empty id also means "cannot go stale".
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
