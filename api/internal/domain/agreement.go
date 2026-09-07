package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The caps, counted in RUNES below and never in bytes: len() is bytes, and a household writing Chinese would
// get a third of what the field promises -- the mistake MaxVisionThemeLen shipped with once.
const (
	MaxAgreementBodyLen = 500
	MaxAgreementNoteLen = 500
	// Its own constant, never an alias of the note's: they are different fields on different screens, and one
	// cap serving both would have to move for both. Nothing in this package reads it -- AgreementService.Park
	// enforces it and ErrAgreementParkNoteTooLong is its refusal -- so it is declared here, beside its
	// siblings, rather than in usecase where a domain cap does not belong.
	MaxAgreementParkNoteLen    = 500
	MaxAgreementSectionNameLen = 60
	// A minimum, never a maximum: nothing caps a household at two owners, and two is the floor below which
	// nobody can be asked to sign (decision 1).
	MinAgreementOwners = 2
)

// AgreementProposalKind is which of three changes a proposal asks for.
type AgreementProposalKind string

const (
	ProposalAdd    AgreementProposalKind = "add"
	ProposalEdit   AgreementProposalKind = "edit"
	ProposalRemove AgreementProposalKind = "remove"
)

// ParseAgreementProposalKind refuses a kind this code did not construct. A kind arrives from a request body as
// well as a column, and the HTTP handler calls this itself to answer 422 (decision 21), so a corrupt row and a
// caller's typo never share an answer. A fourth kind needs a migration as well as a case here.
func ParseAgreementProposalKind(s string) (AgreementProposalKind, error) {
	switch k := AgreementProposalKind(s); k {
	case ProposalAdd, ProposalEdit, ProposalRemove:
		return k, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownAgreementProposalKind, s)
	}
}

// AgreementProposalStatus is where a proposal stands. There is no fifth: no decline, because an unanswered
// proposal is a conversation that has not happened yet, and no expiry, because silently deleting what one
// partner asked for is the harshest thing this feature could do (decision 6).
type AgreementProposalStatus string

const (
	ProposalPending   AgreementProposalStatus = "pending"
	ProposalParked    AgreementProposalStatus = "parked"
	ProposalAccepted  AgreementProposalStatus = "accepted"
	ProposalWithdrawn AgreementProposalStatus = "withdrawn"
)

// ParseAgreementProposalStatus refuses a status this code did not construct. A status only ever arrives from a
// column, so a refusal here is a corrupt row rather than anyone's mistake.
func ParseAgreementProposalStatus(s string) (AgreementProposalStatus, error) {
	switch st := AgreementProposalStatus(s); st {
	case ProposalPending, ProposalParked, ProposalAccepted, ProposalWithdrawn:
		return st, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownAgreementProposalStatus, s)
	}
}

// IsOpen is pending-or-parked in one function, so a fifth status cannot read as open at three call sites and
// closed at the fourth.
func (s AgreementProposalStatus) IsOpen() bool {
	return s == ProposalPending || s == ProposalParked
}

// StarterSectionNames is what "Use starter set" seeds: four labels and no agreements at all, so "everything
// here is here because you both agreed" stays literally true (decision 17). A fresh slice per call -- a caller
// must not be able to rename this package's own copy.
func StarterSectionNames() []string {
	return []string{"Money", "Conflict", "Home & kids", "Us"}
}

// RequiredSigners is every CURRENT owner's membership id (decision 4). ValidateMembershipChange refuses
// CapMarriage to a limited member, so nobody else can ever be asked to sign.
func RequiredSigners(all []Membership) []string {
	ids := make([]string, 0, len(all))
	for _, m := range all {
		if m.Role == RoleOwner {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// AwaitingSignature is the owners yet to sign, in RequiredSigners' order -- what the card's "needs Christine"
// is built from. The order is load-bearing and pinned by this package's own test, because the service tests
// cannot pin it: their membership double ranges a Go map. A signature held by someone who is no longer an
// owner is ignored here, never deleted: the record of who agreed outlives their membership.
func AwaitingSignature(all []Membership, signed []string) []string {
	has := make(map[string]bool, len(signed))
	for _, id := range signed {
		has[id] = true
	}
	out := make([]string, 0, len(all))
	for _, id := range RequiredSigners(all) {
		if !has[id] {
			out = append(out, id)
		}
	}
	return out
}

// AgreementsLocked is decision 1's gate, named once so the read path's "is this screen locked" and every write
// path's "may this write happen" cannot drift apart.
func AgreementsLocked(all []Membership) bool {
	return len(RequiredSigners(all)) < MinAgreementOwners
}

// AgreementDisplayNumbers is the design's 01..12. sizes[i] is section i's live count, and the numbering runs
// CONTINUOUSLY across sections -- Money ends at 04 and Conflict starts at 05 -- which is why this takes the
// whole document's shape rather than one section's. Integers, never pre-padded strings: the zero padding is
// presentation and the browser owns it. Derived on every read; storing a number would mean renumbering every
// later agreement on every removal (decision 11).
func AgreementDisplayNumbers(sizes []int) [][]int {
	out := make([][]int, 0, len(sizes))
	// Declared OUTSIDE the loop: this single counter is the whole of the continuity rule. Moved inside, every
	// section restarts at 1 and the document numbers 01,02 / 01,02,03.
	next := 1
	for _, size := range sizes {
		// max(size, 0): the caller counts its own rows, so a negative is a bug -- and make would panic on it.
		numbers := make([]int, 0, max(size, 0))
		for i := 0; i < size; i++ {
			numbers = append(numbers, next)
			next++
		}
		out = append(out, numbers)
	}
	return out
}

// AgreementProposal is one proposed change, before any row exists. The service stamps HouseholdID and
// ProposedByMembershipID from the route and the session, never from a request body.
type AgreementProposal struct {
	HouseholdID            string
	Kind                   string
	SectionID              string
	TargetAgreementID      string
	Body                   string
	PreviousBody           string
	Note                   string
	ProposedByMembershipID string
}

// Validate is every rule that needs no database, run before any repository call so an invalid proposal writes
// nothing. It never rewrites a field -- the service trims on the way in, so what is stored is what was
// validated -- and it measures the trimmed text for that same reason: a 500-rune body with a trailing newline
// must not be refused for a length the stored row will not have.
func (p AgreementProposal) Validate() error {
	if utf8.RuneCountInString(strings.TrimSpace(p.Note)) > MaxAgreementNoteLen {
		return ErrAgreementNoteTooLong
	}
	// Fail closed: an unrecognised kind is refused rather than falling through to a default shape. It cannot
	// arrive from a request -- the handler parses that itself and answers 422 -- so the default arm below is a
	// bug, and an unmapped sentinel logging a 500 is the right answer to one.
	switch AgreementProposalKind(p.Kind) {
	case ProposalAdd:
		// PreviousBody is the target's wording, and an add has no target.
		if p.SectionID == "" || p.TargetAgreementID != "" || p.PreviousBody != "" {
			return ErrAgreementProposalShapeInvalid
		}
		return validateAgreementBody(p.Body)
	case ProposalEdit:
		// No caller-supplied section: CreateProposal copies the target's own, in the statement that verifies it.
		if p.TargetAgreementID == "" || p.SectionID != "" || p.PreviousBody == "" {
			return ErrAgreementProposalShapeInvalid
		}
		if err := validateAgreementBody(p.Body); err != nil {
			return err
		}
		// The version number is a promise that something happened.
		if strings.TrimSpace(p.Body) == strings.TrimSpace(p.PreviousBody) {
			return ErrAgreementEditUnchanged
		}
		return nil
	case ProposalRemove:
		if p.TargetAgreementID == "" || p.SectionID != "" || p.PreviousBody == "" ||
			strings.TrimSpace(p.Body) != "" {
			return ErrAgreementProposalShapeInvalid
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownAgreementProposalKind, p.Kind)
	}
}

// validateAgreementBody is the body's own two rules, shared by add and edit so the two kinds cannot drift
// apart on what a body may be.
func validateAgreementBody(body string) error {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return ErrAgreementBodyRequired
	}
	if utf8.RuneCountInString(trimmed) > MaxAgreementBodyLen {
		return ErrAgreementBodyTooLong
	}
	return nil
}

// ValidateAgreementSectionName is the New-section modal's rule. There is no cap on how many sections a
// household may have: a count cap is a check-then-write two owners can both pass.
func ValidateAgreementSectionName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ErrAgreementSectionNameRequired
	}
	if utf8.RuneCountInString(trimmed) > MaxAgreementSectionNameLen {
		return ErrAgreementSectionNameTooLong
	}
	return nil
}
