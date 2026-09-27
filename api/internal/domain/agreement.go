package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Don't count these caps with len(): it counts bytes, not runes, and a
// household writing Chinese would get only a third of what the field
// promises.
const (
	MaxAgreementBodyLen = 500
	MaxAgreementNoteLen = 500
	// Its own constant, not an alias of the note's: different fields, different
	// screens. Nothing here reads it; AgreementService.Park enforces it via
	// ErrAgreementParkNoteTooLong. It sits beside its siblings because a domain
	// cap doesn't belong in usecase.
	MaxAgreementParkNoteLen    = 500
	MaxAgreementSectionNameLen = 60
	// A minimum, never a maximum: nothing caps a household at two owners,
	// and two is the floor below which nobody can be asked to sign.
	MinAgreementOwners = 2
)

// AgreementProposalKind is which of three changes a proposal asks for.
type AgreementProposalKind string

const (
	ProposalAdd    AgreementProposalKind = "add"
	ProposalEdit   AgreementProposalKind = "edit"
	ProposalRemove AgreementProposalKind = "remove"
)

// ParseAgreementProposalKind refuses a kind this code did not construct. The
// HTTP handler calls this itself and answers 422, so a corrupt row and a
// caller's typo never share an answer. A fourth kind needs a migration as
// well as a case.
func ParseAgreementProposalKind(s string) (AgreementProposalKind, error) {
	switch k := AgreementProposalKind(s); k {
	case ProposalAdd, ProposalEdit, ProposalRemove:
		return k, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownAgreementProposalKind, s)
	}
}

// AgreementProposalStatus is where a proposal stands. There is no fifth: no
// decline, because an unanswered proposal is a conversation that has not
// happened yet, and no expiry, because silently deleting what one partner
// asked for is the harshest thing this feature could do.
type AgreementProposalStatus string

const (
	ProposalPending   AgreementProposalStatus = "pending"
	ProposalParked    AgreementProposalStatus = "parked"
	ProposalAccepted  AgreementProposalStatus = "accepted"
	ProposalWithdrawn AgreementProposalStatus = "withdrawn"
)

// ParseAgreementProposalStatus refuses a status this code did not
// construct. A status only ever arrives from a column, so a refusal here is
// a corrupt row rather than anyone's mistake.
func ParseAgreementProposalStatus(s string) (AgreementProposalStatus, error) {
	switch st := AgreementProposalStatus(s); st {
	case ProposalPending, ProposalParked, ProposalAccepted, ProposalWithdrawn:
		return st, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownAgreementProposalStatus, s)
	}
}

// IsOpen is pending-or-parked in one function, so a fifth status cannot
// read as open at three call sites and closed at the fourth.
func (s AgreementProposalStatus) IsOpen() bool {
	return s == ProposalPending || s == ProposalParked
}

// StarterSectionNames is what "Use starter set" seeds: four labels and no
// agreements at all, so "everything here is here because you both agreed"
// stays literally true. A fresh slice per call -- a caller must not be able
// to rename this package's own copy.
func StarterSectionNames() []string {
	return []string{"Money", "Conflict", "Home & kids", "Us"}
}

// RequiredSigners is every current owner's membership id, evaluated live: a
// joiner must sign an existing proposal, a leaver stops blocking it.
// ValidateMembershipChange refuses CapMarriage to a limited member, so nobody
// else can ever be asked to sign.
func RequiredSigners(all []Membership) []string {
	ids := make([]string, 0, len(all))
	for _, m := range all {
		if m.Role == RoleOwner {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// AwaitingSignature is the owners yet to sign, in RequiredSigners' order --
// what the card's "needs Christine" is built from. That order is pinned only
// by this package's own test, because the service tests' membership double
// ranges a Go map. A non-owner's signature is ignored here, never deleted:
// the record of who agreed outlives their membership.
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

// AgreementsLocked reports whether the household has fewer than
// MinAgreementOwners owners. Named once so the read path's "is this locked"
// and every write path's "may this write happen" can't drift apart.
func AgreementsLocked(all []Membership) bool {
	return len(RequiredSigners(all)) < MinAgreementOwners
}

// AgreementDisplayNumbers is the design's 01..12: sizes[i] is section i's
// live count. Numbering runs CONTINUOUSLY across sections -- Money ends 04,
// Conflict starts 05 -- so it takes the whole document's shape, not one
// section's. It returns integers, not pre-padded strings: padding is the
// browser's job. Derived on every read, since storing a number would mean
// renumbering every later agreement on every removal.
func AgreementDisplayNumbers(sizes []int) [][]int {
	out := make([][]int, 0, len(sizes))
	// Declared OUTSIDE the loop: this single counter is the whole of the
	// continuity rule. Moved inside, every section restarts at 1 and the
	// document numbers 01,02 / 01,02,03.
	next := 1
	for _, size := range sizes {
		// max(size, 0): the caller counts its own rows, so a negative is a
		// bug -- and make would panic on it.
		numbers := make([]int, 0, max(size, 0))
		for i := 0; i < size; i++ {
			numbers = append(numbers, next)
			next++
		}
		out = append(out, numbers)
	}
	return out
}

// AgreementProposal is one proposed change, before any row exists. The
// service stamps HouseholdID and ProposedByMembershipID from the route and
// the session, never from a request body.
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

// Validate is every DB-free rule, run before any repository call so an
// invalid proposal writes nothing. It never rewrites a field -- the service
// trims on input, so stored equals validated -- and it measures the trimmed
// text for the same reason: a trailing-newline body isn't refused for a
// length the stored row won't have.
func (p AgreementProposal) Validate() error {
	if utf8.RuneCountInString(strings.TrimSpace(p.Note)) > MaxAgreementNoteLen {
		return ErrAgreementNoteTooLong
	}
	// Fail closed: an unrecognised kind is refused, never falls through to a
	// default shape. It can't arrive from a request -- the handler parses and
	// 422s that itself -- so reaching the default arm is a bug, and a logged
	// 500 is the right answer to one.
	switch AgreementProposalKind(p.Kind) {
	case ProposalAdd:
		// PreviousBody is the target's wording, and an add has no target.
		if p.SectionID == "" || p.TargetAgreementID != "" || p.PreviousBody != "" {
			return ErrAgreementProposalShapeInvalid
		}
		return validateAgreementBody(p.Body)
	case ProposalEdit:
		// No caller-supplied section: CreateProposal copies the target's
		// own, in the statement that verifies it.
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

// validateAgreementBody is the body's own two rules, shared by add and edit
// so the two kinds cannot drift apart on what a body may be.
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

// ValidateAgreementSectionName is the New-section modal's rule. There is no
// cap on how many sections a household may have: a count cap is a
// check-then-write two owners can both pass.
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
