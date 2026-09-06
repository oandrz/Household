package usecase_test

import (
	"context"
	"fmt"
	"testing"
	"time"

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
