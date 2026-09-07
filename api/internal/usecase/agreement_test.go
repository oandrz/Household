package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// seedMembers builds the household every agreement test runs against: the
// given number of owners named in that order, plus a limited member who can
// never be asked to sign (domain.ValidateMembershipChange refuses CapMarriage
// to one). Without that member the signing-set helpers get tested minus their
// filter.
func seedMembers(t *testing.T, owners int) *membershipDouble {
	t.Helper()
	members := newMembershipDouble(newUserDouble())
	add := func(id, name string, role domain.Role) {
		members.users.put(usecase.StoredUser{User: domain.User{ID: "u-" + id, DisplayName: name}})
		members.put(domain.Membership{ID: id, HouseholdID: "hh", UserID: "u-" + id, Role: role})
	}
	for i, name := range []string{"Andreas", "Christine", "Priya"}[:owners] {
		add(fmt.Sprintf("m%d", i+1), name, domain.RoleOwner)
	}
	add("kid", "Kiddo", domain.RoleLimited)
	return members
}

func TestAgreementGetComposesTheWholeDocument(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money, home := repo.seedSection("Money"), repo.seedSection("Home & kids")
	repo.seedSection("Us") // no live agreement: invisible, but the picker still offers it
	repo.seedAgreement(money.ID, "Anything over $200 gets a conversation")
	repo.seedAgreement(money.ID, "We review the budget on the first Sunday")
	repo.seedAgreement(home.ID, "Phone-free dinners")
	june := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	august := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	september := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	repo.seedProposal("accepted", "add", money.ID, "Anything over $200 gets a conversation", &june, "m1", "m2")
	repo.seedProposal("accepted", "add", money.ID, "We review the budget on the first Sunday", &august, "m1", "m2")
	repo.seedProposal("accepted", "add", home.ID, "Phone-free dinners", &september, "m1", "m2")
	repo.seedProposal("pending", "add", home.ID, "One night a week is ours", nil, "m1")
	repo.seedProposal("parked", "add", money.ID, "We split the holiday fund", nil, "m1")
	repo.seedProposal("withdrawn", "add", money.ID, "Never mind", &september, "m1")

	view, err := usecase.NewAgreementService(repo, members).Get(context.Background(), "hh")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// count(ACCEPTED) + 1 (decision 10): the pending, parked and withdrawn
	// rows count toward nothing.
	if view.Version != 4 {
		t.Errorf("Version = %d, want 4 -- count(accepted) + 1, not count(*)", view.Version)
	}
	if view.UpdatedAt == nil || !view.UpdatedAt.Equal(september) {
		t.Errorf("UpdatedAt = %v, want the newest acceptance %v", view.UpdatedAt, september)
	}
	if len(view.Proposals) != 2 {
		t.Errorf("len(Proposals) = %d, want 2 -- pending and parked only", len(view.Proposals))
	}
	// Numbering runs continuously ACROSS sections: Money 01-02, Home 03.
	if got := view.Sections[1].Agreements[0].Number; got != 3 {
		t.Errorf("Home's first agreement is number %d, want 3 -- numbering does not restart", got)
	}
	if view.Sections[0].Count != 2 || !view.Sections[0].Visible {
		t.Errorf("Money = {Count %d, Visible %v}, want {2, true}", view.Sections[0].Count, view.Sections[0].Visible)
	}
	// An empty section travels with Visible false rather than being dropped:
	// the page hides it, the propose picker still offers it (decision 8).
	if view.Sections[2].Visible || view.Sections[2].Count != 0 {
		t.Errorf("Us = {Count %d, Visible %v}, want {0, false} and still present",
			view.Sections[2].Count, view.Sections[2].Visible)
	}
	// Every open proposal carries its section's NAME, not only its id: the
	// card prints it and the write responses reuse the same composition.
	if view.Proposals[0].SectionName != "Home & kids" {
		t.Errorf("the pending proposal's SectionName = %q, want %q", view.Proposals[0].SectionName, "Home & kids")
	}
	// An add has no target, so it can never be stale (decision 14).
	if view.Proposals[0].TargetChanged {
		t.Error("TargetChanged = true on an add -- an add has no target to go stale")
	}
	// History is the accepted slice reversed, newest first, each numbered
	// with the version that change PRODUCED.
	if len(view.History) != 3 || view.History[0].Version != 4 || view.History[2].Version != 2 {
		t.Fatalf("history versions = %+v, want v4, v3, v2", view.History)
	}
	if got := view.History[0].SignedByNames; len(got) != 2 || got[0] != "Andreas" {
		t.Errorf("SignedByNames = %v, want both owners", got)
	}
	if view.History[0].SectionName != "Home & kids" {
		t.Errorf("history[0].SectionName = %q, want %q", view.History[0].SectionName, "Home & kids")
	}
}

// A household down to one owner keeps seeing everything it agreed to and its
// frozen proposals (decision 3): Locked gates writes, it never hides rows.
func TestAgreementGetIsLockedForOneOwnerAndStillCarriesTheDocument(t *testing.T) {
	members := seedMembers(t, 1)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money := repo.seedSection("Money")
	repo.seedAgreement(money.ID, "Anything over $200 gets a conversation")
	repo.seedProposal("pending", "add", money.ID, "We split the holiday fund", nil, "m1")

	view, err := usecase.NewAgreementService(repo, members).Get(context.Background(), "hh")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !view.Locked {
		t.Fatal("Locked = false on a one-owner household")
	}
	if len(view.Owners) != 1 || len(view.Sections[0].Agreements) != 1 || len(view.Proposals) != 1 {
		t.Fatalf("the locked document lost rows: %d owners, %d agreements, %d proposals",
			len(view.Owners), len(view.Sections[0].Agreements), len(view.Proposals))
	}
}

// UpdatedAt is nil before anything is accepted, so the header can say nothing
// about when the document last moved rather than claiming it moved at the
// zero time. Neither a pending nor a withdrawn proposal is an acceptance.
func TestAgreementGetHasNoUpdatedAtUntilSomethingIsAccepted(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money := repo.seedSection("Money")
	withdrawnAt := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	repo.seedProposal("pending", "add", money.ID, "We split the holiday fund", nil, "m1")
	repo.seedProposal("withdrawn", "add", money.ID, "Never mind", &withdrawnAt, "m1")

	view, err := usecase.NewAgreementService(repo, members).Get(context.Background(), "hh")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.UpdatedAt != nil {
		t.Errorf("UpdatedAt = %v, want nil -- nothing has been accepted", view.UpdatedAt)
	}
	if view.Version != 1 || len(view.History) != 0 {
		t.Errorf("v%d with %d history entries, want v1 and none", view.Version, len(view.History))
	}
}

// Every write refuses a one-owner household, and refuses it BEFORE any
// repository call: the write counter is what separates "refused" from
// "refused eventually" (decision 1). Uniform on purpose -- a rule that let
// some writes through a locked household is one a reader gets wrong.
func TestAgreementWritesAreRefusedOnAOneOwnerHouseholdBeforeAnyRepositoryCall(t *testing.T) {
	members := seedMembers(t, 1)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	section := repo.seedSection("Money")
	open := repo.seedProposal("pending", "add", section.ID, "We split the holiday fund", nil, "m1")
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	for name, write := range map[string]func() error{
		"CreateSection": func() error { _, _, err := svc.CreateSection(ctx, "hh", "Faith", now); return err },
		"SeedStarterSections": func() error {
			_, err := svc.SeedStarterSections(ctx, "hh", now)
			return err
		},
		"Propose": func() error {
			_, _, err := svc.Propose(ctx, "hh", "m1", domain.AgreementProposal{
				Kind: "add", SectionID: section.ID, Body: "One night a week is ours"}, now)
			return err
		},
		"Sign":     func() error { _, _, err := svc.Sign(ctx, "hh", open.ID, "m1", now); return err },
		"Park":     func() error { _, _, err := svc.Park(ctx, "hh", open.ID, "next retro", now); return err },
		"Withdraw": func() error { _, _, err := svc.Withdraw(ctx, "hh", open.ID, "m1", now); return err },
	} {
		if err := write(); !errors.Is(err, domain.ErrAgreementsNeedTwoOwners) {
			t.Errorf("%s: err = %v, want ErrAgreementsNeedTwoOwners", name, err)
		}
	}
	if repo.writes != 0 {
		t.Fatalf("%d writes reached the repository, want 0 -- the gate ran after the call", repo.writes)
	}
}

// Proposing is agreeing (decision 5), and the signing set is every CURRENT
// owner (decision 4): an owner who joins mid-proposal must sign, one who
// leaves stops blocking it, and the proposal stays open through both. The
// write returns the row AND the recomposed document, and both are asserted
// here, because Task 7 maps the first and Task 8 answers with the second.
func TestAgreementProposeSignsTheProposerAndTheAwaitingListFollowsTheOwners(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	section := repo.seedSection("Us")
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	proposal, doc, err := svc.Propose(ctx, "hh", "m1", domain.AgreementProposal{
		Kind: "add", SectionID: section.ID, Body: "  One night a week is ours  "}, now)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if proposal.Body != "One night a week is ours" {
		t.Errorf("Body = %q -- the service trims before it validates, so what is stored is what was checked", proposal.Body)
	}
	if proposal.Status != "pending" || proposal.SectionName != "Us" || proposal.ProposedByName != "Andreas" {
		t.Errorf("the written row = {Status %q, SectionName %q, ProposedByName %q}, want {pending, Us, Andreas}",
			proposal.Status, proposal.SectionName, proposal.ProposedByName)
	}
	// The proposer's own signature landed with the proposal, so only the
	// other owner is awaited -- the design's "needs Christine". Both halves
	// of the response say so, from the one composition.
	if got := proposal.AwaitingNames; len(got) != 1 || got[0] != "Christine" {
		t.Fatalf("the written row awaits %v, want [Christine] -- the proposer signed implicitly", got)
	}
	if len(doc.Proposals) != 1 || doc.Proposals[0].ID != proposal.ID {
		t.Fatalf("the document returned with the write carries %d proposals, want the one just written", len(doc.Proposals))
	}
	if doc.Version != 1 {
		t.Errorf("Version = %d, want 1 -- a proposal nobody has agreed to has changed nothing", doc.Version)
	}

	awaiting := func() []string {
		t.Helper()
		view, err := svc.Get(ctx, "hh")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if len(view.Proposals) != 1 {
			t.Fatalf("len(Proposals) = %d, want 1 -- it waits, it is never filed away", len(view.Proposals))
		}
		return view.Proposals[0].AwaitingNames
	}
	members.users.put(usecase.StoredUser{User: domain.User{ID: "u-m3", DisplayName: "Priya"}})
	members.put(domain.Membership{ID: "m3", HouseholdID: "hh", UserID: "u-m3", Role: domain.RoleOwner})
	// Length only, never got[0]: membershipDouble.List ranges a Go map, so
	// the order domain.RequiredSigners sees here is not stable. The order
	// claim is the domain test's to pin, on a fixture it builds itself.
	if got := awaiting(); len(got) != 2 {
		t.Fatalf("awaiting = %v, want both non-proposers -- an owner who joins is bound by the promise", got)
	}
	if err := members.Delete(ctx, "hh", "m3"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := awaiting(); len(got) != 1 || got[0] != "Christine" {
		t.Fatalf("awaiting = %v, want [Christine] -- an owner who leaves stops blocking it", got)
	}

	// The last awaiting signature applies the change: the proposal leaves the
	// open list, the version moves, and the agreement is numbered in place.
	signed, after, err := svc.Sign(ctx, "hh", proposal.ID, "m2", now)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if signed.Status != "accepted" || signed.TargetChanged {
		t.Errorf("the completing signature returned {Status %q, TargetChanged %v}, want {accepted, false}",
			signed.Status, signed.TargetChanged)
	}
	if len(after.Proposals) != 0 || after.Version != 2 || len(after.Sections[0].Agreements) != 1 {
		t.Fatalf("after the completing signature: %d open, v%d, %d agreements -- want 0, v2, 1",
			len(after.Proposals), after.Version, len(after.Sections[0].Agreements))
	}
	if got := after.Sections[0].Agreements[0].Number; got != 1 {
		t.Errorf("the new agreement is number %d, want 1", got)
	}
}

// The last awaiting signature applies an EDIT and a REMOVE, not only an add.
// An edit is a remove and an add together, so the agreement's id changes and
// the new wording sorts last in its section, exactly where created_at, id
// puts it -- which is why the assertions below find rows by body. The
// proposal keeps the OLD wording in PreviousBody, because that is what the
// history modal renders and what Restore pre-fills (decisions 13 and 18).
func TestAgreementSignAppliesAnEditAndARemoveAndRenumbersWhatIsLeft(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money, home := repo.seedSection("Money"), repo.seedSection("Home & kids")
	overTwoHundred := repo.seedAgreement(money.ID, "Anything over $200 gets a conversation")
	firstSunday := repo.seedAgreement(money.ID, "We review the budget on the first Sunday")
	repo.seedAgreement(home.ID, "Phone-free dinners")
	svc := usecase.NewAgreementService(repo, members)
	ctx := context.Background()
	proposedAt := time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)
	agreedAt := proposedAt.Add(time.Hour)

	edit, _, err := svc.Propose(ctx, "hh", "m1", domain.AgreementProposal{
		Kind:              "edit",
		TargetAgreementID: overTwoHundred.ID,
		PreviousBody:      overTwoHundred.Body,
		Body:              "Anything over $300 gets a conversation"}, proposedAt)
	if err != nil {
		t.Fatalf("Propose an edit: %v", err)
	}
	// The section came from the TARGET, not the caller: Validate refuses a
	// caller-supplied one on an edit, so this can only have been copied.
	if edit.SectionID != money.ID || edit.SectionName != "Money" {
		t.Fatalf("the edit landed in section %q (%q), want Money -- the target's section is copied onto the proposal",
			edit.SectionName, edit.SectionID)
	}
	if _, _, err := svc.Sign(ctx, "hh", edit.ID, "m2", agreedAt); err != nil {
		t.Fatalf("Sign the edit: %v", err)
	}

	remove, _, err := svc.Propose(ctx, "hh", "m1", domain.AgreementProposal{
		Kind:              "remove",
		TargetAgreementID: firstSunday.ID,
		PreviousBody:      firstSunday.Body}, proposedAt)
	if err != nil {
		t.Fatalf("Propose a remove: %v", err)
	}
	removed, doc, err := svc.Sign(ctx, "hh", remove.ID, "m2", agreedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("Sign the remove: %v", err)
	}
	if removed.Status != "accepted" || removed.TargetChanged {
		t.Errorf("the completing signature returned {Status %q, TargetChanged %v}, want {accepted, false} -- "+
			"a resolved proposal cannot be stale, and an accepted remove has taken its own target away",
			removed.Status, removed.TargetChanged)
	}

	bodies := func(sec usecase.AgreementSectionView) []string {
		out := make([]string, 0, len(sec.Agreements))
		for _, a := range sec.Agreements {
			out = append(out, a.Body)
		}
		return out
	}
	if got := bodies(doc.Sections[0]); len(got) != 1 || got[0] != "Anything over $300 gets a conversation" {
		t.Fatalf("Money holds %v, want only the edited wording -- the edit replaced a row, it did not add one", got)
	}
	// Numbering runs continuously across sections and counts only live rows:
	// Money 01, Home 02, with two removed rows numbered nowhere.
	if doc.Sections[0].Agreements[0].Number != 1 || doc.Sections[1].Agreements[0].Number != 2 {
		t.Errorf("numbers are %d then %d, want 1 then 2 -- a removed row is not numbered and not skipped over",
			doc.Sections[0].Agreements[0].Number, doc.Sections[1].Agreements[0].Number)
	}
	if doc.Version != 3 || len(doc.History) != 2 {
		t.Fatalf("v%d with %d history entries, want v3 and 2", doc.Version, len(doc.History))
	}
	// History is newest first, and a removal renders wording that is nowhere
	// else in the document any more -- "you can always see it was there and
	// restore it later".
	if doc.History[0].Kind != "remove" || doc.History[0].Body != "" ||
		doc.History[0].PreviousBody != "We review the budget on the first Sunday" {
		t.Errorf("history[0] = {Kind %q, Body %q, PreviousBody %q}, want the removal carrying the old wording",
			doc.History[0].Kind, doc.History[0].Body, doc.History[0].PreviousBody)
	}
	if doc.History[1].Kind != "edit" || doc.History[1].PreviousBody != "Anything over $200 gets a conversation" {
		t.Errorf("history[1] = {Kind %q, PreviousBody %q}, want the edit carrying the wording it replaced",
			doc.History[1].Kind, doc.History[1].PreviousBody)
	}
}

// Withdrawing something already accepted is the last signer double-clicking
// through a stale page. It means "reload, this was settled", and it must
// write nothing: an accepted proposal that flipped to withdrawn would leave
// its agreement live with no record of how it got there.
func TestAgreementWithdrawOnAnAcceptedProposalWritesNothing(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money := repo.seedSection("Money")
	repo.seedAgreement(money.ID, "Anything over $200 gets a conversation")
	resolved := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	accepted := repo.seedProposal("accepted", "add", money.ID,
		"Anything over $200 gets a conversation", &resolved, "m1", "m2")
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	before := repo.writes
	_, _, err := svc.Withdraw(ctx, "hh", accepted.ID, "m1", now)
	if !errors.Is(err, domain.ErrAgreementNotOpen) {
		t.Fatalf("err = %v, want ErrAgreementNotOpen -- reload, this was settled", err)
	}
	if repo.writes != before {
		t.Errorf("writes moved from %d to %d -- a refusal changed a row", before, repo.writes)
	}
	view, err := svc.Get(ctx, "hh")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.Version != 2 || len(view.History) != 1 {
		t.Errorf("v%d with %d history entries, want v2 and 1 -- the refused withdraw rewrote history", view.Version, len(view.History))
	}
}

// Withdraw hands byMembershipID to the store and never branches on it: the
// proposer check is the HANDLER's, because only the HTTP layer knows who is
// asking (decision 15, inside decision 22's 404 -> 403 -> 409 order). The
// proof is not that a stranger is refused -- it is that the refusal came back
// FROM the repository, which means the service reached it.
func TestAgreementWithdrawHandsTheProposerCheckToTheStore(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money := repo.seedSection("Money")
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	proposed, _, err := svc.Propose(ctx, "hh", "m1", domain.AgreementProposal{
		Kind: "add", SectionID: money.ID, Body: "We split the holiday fund"}, now)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, _, err := svc.Withdraw(ctx, "hh", proposed.ID, "m2", now); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden -- the store's own backstop clause", err)
	}
	if repo.lastWithdrawBy != "m2" {
		t.Fatalf("the repository saw byMembershipID %q, want %q -- the service decided instead of asking",
			repo.lastWithdrawBy, "m2")
	}

	withdrawn, doc, err := svc.Withdraw(ctx, "hh", proposed.ID, "m1", now)
	if err != nil {
		t.Fatalf("Withdraw by the proposer: %v", err)
	}
	if withdrawn.Status != "withdrawn" || len(doc.Proposals) != 0 {
		t.Errorf("the written row is %q with %d open proposals, want withdrawn and 0",
			withdrawn.Status, len(doc.Proposals))
	}
	if doc.Version != 1 || len(doc.History) != 0 {
		t.Errorf("v%d with %d history entries, want v1 and none -- a withdrawal is not a change to the document",
			doc.Version, len(doc.History))
	}
}

// "Use starter set" seeds four labels and nothing else (decision 17), so
// "everything on this page is here because you both agreed" stays literally
// true. A second click is a no-op, not a 409: ON CONFLICT DO NOTHING, and the
// write counter is what proves it.
func TestAgreementStarterSetIsIdempotentAndCreatesNoAgreements(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	first, err := svc.SeedStarterSections(ctx, "hh", now)
	if err != nil {
		t.Fatalf("SeedStarterSections: %v", err)
	}
	want := domain.StarterSectionNames()
	if len(first.Sections) != len(want) {
		t.Fatalf("%d sections, want %d", len(first.Sections), len(want))
	}
	for i, sec := range first.Sections {
		if sec.Name != want[i] {
			t.Errorf("section %d is %q, want %q", i, sec.Name, want[i])
		}
		if sec.Count != 0 || sec.Visible || len(sec.Agreements) != 0 {
			t.Errorf("%q arrived with %d agreements -- the starter set seeds labels, never promises",
				sec.Name, sec.Count)
		}
	}
	if first.Version != 1 {
		t.Errorf("Version = %d, want 1 -- seeding sections agrees to nothing", first.Version)
	}

	afterFirst := repo.writes
	second, err := svc.SeedStarterSections(ctx, "hh", now)
	if err != nil {
		t.Fatalf("SeedStarterSections a second time: %v -- a second click is a no-op, not a 409", err)
	}
	if len(second.Sections) != len(want) {
		t.Errorf("%d sections after the second click, want %d -- it duplicated or dropped labels", len(second.Sections), len(want))
	}
	if repo.writes != afterFirst {
		t.Errorf("%d writes on the second click, want 0", repo.writes-afterFirst)
	}
}

// The park note is capped separately from the proposal note: two fields on
// two screens, and one constant serving both would have to move for both.
// Both constants are 500, so no length assertion can tell them apart -- only
// the SENTINEL can, which is why this test asserts one and refuses the other.
func TestAgreementParkCapsItsOwnNoteSeparatelyFromTheProposalNote(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money := repo.seedSection("Money")
	open := repo.seedProposal("pending", "add", money.ID, "We split the holiday fund", nil, "m1")
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	// Three bytes to the rune, so a byte cap and a rune cap disagree about
	// this fixture: len() refuses the note that must be accepted.
	atCap := strings.Repeat("我", domain.MaxAgreementParkNoteLen)

	parked, _, err := svc.Park(ctx, "hh", open.ID, "  "+atCap+"  ", now)
	if err != nil {
		t.Fatalf("Park at the cap: %v -- %d runes is inside MaxAgreementParkNoteLen whatever it is in bytes",
			err, domain.MaxAgreementParkNoteLen)
	}
	if utf8.RuneCountInString(parked.ParkNote) != domain.MaxAgreementParkNoteLen {
		t.Errorf("stored park note is %d runes, want %d -- trimmed before it was measured",
			utf8.RuneCountInString(parked.ParkNote), domain.MaxAgreementParkNoteLen)
	}
	if parked.Status != "parked" {
		t.Errorf("Status = %q, want parked -- Discuss leaves the proposal open (decision 7)", parked.Status)
	}

	writes := repo.writes
	_, _, err = svc.Park(ctx, "hh", open.ID, atCap+"我", now)
	if !errors.Is(err, domain.ErrAgreementParkNoteTooLong) {
		t.Errorf("err = %v, want ErrAgreementParkNoteTooLong", err)
	}
	if errors.Is(err, domain.ErrAgreementNoteTooLong) {
		t.Error("Park answered ErrAgreementNoteTooLong -- a tidy-up aliased the park note's cap to the proposal note's, " +
			"which no length assertion can see because both constants are 500")
	}
	if repo.writes != writes {
		t.Errorf("writes moved from %d to %d -- an over-long note reached the repository", writes, repo.writes)
	}
}
