package domain_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// proposal is the valid fixture for one kind, with any mutation applied.
func proposal(kind string, mutate ...func(*domain.AgreementProposal)) domain.AgreementProposal {
	p := domain.AgreementProposal{HouseholdID: "h1", Kind: kind, SectionID: "s1",
		Body: "We review the budget on the first Sunday.", ProposedByMembershipID: "m1"}
	switch kind {
	case "edit":
		p.SectionID, p.TargetAgreementID, p.PreviousBody = "", "a1", "We review the budget on Sundays."
	case "remove":
		p.SectionID, p.TargetAgreementID, p.Body, p.PreviousBody = "", "a1", "", "We review the budget on Sundays."
	}
	for _, m := range mutate {
		m(&p)
	}
	return p
}

// Both parsers refuse the empty value, a capitalised one, a kind that sounds plausible, one with a trailing
// space, and the two statuses decision 6 deliberately lacks -- the refusing default is the whole point of them.
func TestAgreementParsersRefuseWhatNoMigrationAllows(t *testing.T) {
	for _, s := range []string{"", "Add", "delete", "remove ", "declined", "expired"} {
		if _, err := domain.ParseAgreementProposalKind(s); !errors.Is(err, domain.ErrUnknownAgreementProposalKind) {
			t.Errorf("ParseAgreementProposalKind(%q) err = %v, want ErrUnknownAgreementProposalKind", s, err)
		}
		if _, err := domain.ParseAgreementProposalStatus(s); !errors.Is(err, domain.ErrUnknownAgreementProposalStatus) {
			t.Errorf("ParseAgreementProposalStatus(%q) err = %v, want ErrUnknownAgreementProposalStatus", s, err)
		}
	}
	for _, s := range []string{"add", "edit", "remove"} {
		if got, err := domain.ParseAgreementProposalKind(s); err != nil || string(got) != s {
			t.Errorf("ParseAgreementProposalKind(%q) = %q, %v; want %q, nil", s, got, err, s)
		}
	}
	for _, c := range []struct {
		s    string
		open bool
	}{{"pending", true}, {"parked", true}, {"accepted", false}, {"withdrawn", false}} {
		got, err := domain.ParseAgreementProposalStatus(c.s)
		if err != nil {
			t.Fatalf("ParseAgreementProposalStatus(%q): %v", c.s, err)
		}
		if got.IsOpen() != c.open {
			t.Errorf("%q.IsOpen() = %v, want %v", c.s, got.IsOpen(), c.open)
		}
	}
}

func TestAgreementProposalValidate(t *testing.T) {
	// 500 runes of Chinese is 1500 bytes: the cap counts runes, so this passes.
	body := strings.Repeat("承", domain.MaxAgreementBodyLen)
	cases := []struct {
		name string
		in   domain.AgreementProposal
		want error
	}{
		{"a valid add", proposal("add"), nil},
		{"a valid edit", proposal("edit"), nil},
		{"a valid remove", proposal("remove"), nil},
		{"a body at the rune cap", proposal("add", func(p *domain.AgreementProposal) { p.Body = body }), nil},
		{"a body trimmed back under the cap", proposal("add", func(p *domain.AgreementProposal) { p.Body = " " + body + "\n" }), nil},
		{"a body one rune over", proposal("add", func(p *domain.AgreementProposal) { p.Body = body + "承" }), domain.ErrAgreementBodyTooLong},
		{"a blank body", proposal("add", func(p *domain.AgreementProposal) { p.Body = "   " }), domain.ErrAgreementBodyRequired},
		{"a note at the rune cap", proposal("add", func(p *domain.AgreementProposal) {
			p.Note = strings.Repeat("承", domain.MaxAgreementNoteLen)
		}), nil},
		{"a note one rune over", proposal("add", func(p *domain.AgreementProposal) {
			p.Note = strings.Repeat("承", domain.MaxAgreementNoteLen+1)
		}), domain.ErrAgreementNoteTooLong},
		{"an add with no section", proposal("add", func(p *domain.AgreementProposal) { p.SectionID = "" }), domain.ErrAgreementProposalShapeInvalid},
		{"an edit with no target", proposal("edit", func(p *domain.AgreementProposal) { p.TargetAgreementID = "" }), domain.ErrAgreementProposalShapeInvalid},
		{"a remove with no previous body", proposal("remove", func(p *domain.AgreementProposal) { p.PreviousBody = "" }), domain.ErrAgreementProposalShapeInvalid},
		{"an edit that changes nothing", proposal("edit", func(p *domain.AgreementProposal) { p.Body = p.PreviousBody }), domain.ErrAgreementEditUnchanged},
		{"a kind no migration allows", proposal("add", func(p *domain.AgreementProposal) { p.Kind = "declined" }), domain.ErrUnknownAgreementProposalKind},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.in.Validate(); !errors.Is(err, c.want) {
				t.Fatalf("Validate() = %v, want %v", err, c.want)
			}
		})
	}
}

func TestValidateAgreementSectionName(t *testing.T) {
	name := strings.Repeat("承", domain.MaxAgreementSectionNameLen)
	cases := []struct {
		in   string
		want error
	}{
		{"Money", nil},
		{name, nil},
		{"  " + name + " ", nil}, // trimmed before it is measured
		{name + "承", domain.ErrAgreementSectionNameTooLong},
		{"   ", domain.ErrAgreementSectionNameRequired},
	}
	for _, c := range cases {
		if err := domain.ValidateAgreementSectionName(c.in); !errors.Is(err, c.want) {
			t.Fatalf("ValidateAgreementSectionName(%q) = %v, want %v", c.in, err, c.want)
		}
	}
}

// MaxAgreementParkNoteLen is DELIBERATELY not asserted here. Nothing in this package reads it -- the park
// note is capped by AgreementService.Park (Task 4) -- and no domain-level assertion can tell a separate
// constant from `MaxAgreementParkNoteLen = MaxAgreementNoteLen`, because an alias is also 500. The one
// assertion that separates them is Task 4's: Park with a 501-rune note returns ErrAgreementParkNoteTooLong
// and NOT ErrAgreementNoteTooLong. If you are here to add a `!= 500` check, it would pass under the alias
// and prove nothing.

// The fixtures carry a limited member on purpose: without one, these three functions are tested minus the
// filter that is all they do. This is also the ONLY place AwaitingSignature's ordering claim is pinned --
// the service tests cannot pin it, because membershipDouble.List ranges a Go map
// (api/internal/usecase/testdouble_test.go:304) and hands back owners in a different order each run.
func TestOwnerHelpersIgnoreLimitedMembers(t *testing.T) {
	owner := func(id string) domain.Membership { return domain.Membership{ID: id, Role: domain.RoleOwner} }
	limited := domain.Membership{ID: "m9", Role: domain.RoleLimited}
	two := []domain.Membership{owner("m1"), limited, owner("m2")}

	if got := domain.RequiredSigners(two); !slices.Equal(got, []string{"m1", "m2"}) {
		t.Fatalf("RequiredSigners = %v, want [m1 m2]", got)
	}
	// Three owners, the middle one signed: the survivors come back in RequiredSigners' order, not the
	// order the signatures arrived in. "needs Christine and Ibu" is built from this slice.
	three := []domain.Membership{owner("m1"), limited, owner("m2"), owner("m3")}
	if got := domain.AwaitingSignature(three, []string{"m2"}); !slices.Equal(got, []string{"m1", "m3"}) {
		t.Fatalf("AwaitingSignature = %v, want [m1 m3]", got)
	}
	// A signature held by someone who is no longer an owner is ignored, never deleted (decision 4).
	if got := domain.AwaitingSignature(two, []string{"m1", "m9"}); !slices.Equal(got, []string{"m2"}) {
		t.Fatalf("AwaitingSignature ignoring a non-owner's signature = %v, want [m2]", got)
	}
	if domain.AgreementsLocked(two) {
		t.Fatal("two owners must not be locked")
	}
	if !domain.AgreementsLocked([]domain.Membership{owner("m1"), limited}) {
		t.Fatal("one owner and a limited member is locked: a limited member can never be asked to sign")
	}
	if got := domain.StarterSectionNames(); !slices.Equal(got, []string{"Money", "Conflict", "Home & kids", "Us"}) {
		t.Fatalf("StarterSectionNames = %v", got)
	}
}

// Numbering runs continuously across sections -- the first ends at 02 and the third starts at 03 -- and an
// empty section takes no number at all.
func TestAgreementDisplayNumbersRunAcrossSections(t *testing.T) {
	got := fmt.Sprint(domain.AgreementDisplayNumbers([]int{2, 0, 3}))
	if got != "[[1 2] [] [3 4 5]]" {
		t.Fatalf("AgreementDisplayNumbers([2 0 3]) = %s, want [[1 2] [] [3 4 5]]", got)
	}
}
