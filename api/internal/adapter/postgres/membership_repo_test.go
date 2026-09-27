package postgres_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// TestMembershipRepoRoundTrip exercises List, ByUser, UpdateWithCheck and
// DeleteWithCheck, none of which repos_test.go's
// TestMembershipRepoRejectsAnInvalidCapabilitySet reaches (that test only
// exercises Create's error path). The checks here accept anything: the
// household rules are the caller's, and the tests further down cover the
// check itself. List in particular
// joins users onto memberships and is the one conversion in this package
// that draws from two row sources at once (row.UserID feeds both
// Membership.UserID and User.ID; Email/DisplayName/AvatarInitial come from
// the joined user) -- a mixed-up field assignment there would still compile
// and would only be caught by an assertion like this one.
func TestMembershipRepoRoundTrip(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	members := postgres.NewMembershipRepo(db)

	h, err := households.Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	u, err := users.Create(ctx, "andreas@hearth.family", "hash", "Andreas")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	created, err := members.Create(ctx, domain.Membership{
		HouseholdID: h.ID, UserID: u.ID, Role: domain.RoleOwner,
		Capabilities: domain.AllCapabilities(),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.HouseholdID != h.ID || created.UserID != u.ID || created.Role != domain.RoleOwner {
		t.Fatalf("created = %+v", created)
	}

	list, err := members.List(ctx, h.ID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	view := list[0]
	if view.Membership.UserID != u.ID || view.User.ID != u.ID {
		t.Fatalf("membership/user id mismatch: %+v", view)
	}
	if view.User.DisplayName != "Andreas" || view.User.Email != "andreas@hearth.family" {
		t.Fatalf("joined user fields wrong: %+v", view.User)
	}
	if len(view.Membership.Capabilities) != 4 || !view.Membership.Capabilities.Has(domain.CapMarriage) {
		t.Fatalf("capabilities = %+v", view.Membership.Capabilities)
	}

	byUser, err := members.ByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("ByUser: %v", err)
	}
	if byUser.ID != created.ID {
		t.Fatalf("ByUser returned a different membership: %+v", byUser)
	}

	if err := members.UpdateWithCheck(ctx, h.ID, created.ID, write(domain.RoleLimited, domain.Capabilities{domain.CapCalendar})); err != nil {
		t.Fatalf("Update: %v", err)
	}
	byUser, err = members.ByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("ByUser after update: %v", err)
	}
	if byUser.Role != domain.RoleLimited || len(byUser.Capabilities) != 1 || byUser.Capabilities[0] != domain.CapCalendar {
		t.Fatalf("after update: %+v", byUser)
	}

	if err := members.DeleteWithCheck(ctx, h.ID, created.ID, allowAny); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	list, err = members.List(ctx, h.ID)
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("len(list) after delete = %d, want 0", len(list))
	}
}

// allowAny is a check that accepts every removal, and write a decide that
// writes the given role and capabilities whatever the household holds. Both
// are for tests about the write itself rather than the rule guarding it.
func allowAny([]domain.Membership) error { return nil }

func write(role domain.Role, caps domain.Capabilities) func([]domain.Membership) (domain.Role, domain.Capabilities, error) {
	return func([]domain.Membership) (domain.Role, domain.Capabilities, error) { return role, caps, nil }
}

// twoOwnerHousehold creates a household whose two members are both owners and
// returns the household ID and the two membership IDs.
func twoOwnerHousehold(t *testing.T, db *postgres.DB) (householdID, firstID, secondID string) {
	t.Helper()
	ctx := context.Background()
	h, err := postgres.NewHouseholdRepo(db).Create(ctx, domain.Household{
		Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR",
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	users := postgres.NewUserRepo(db)
	members := postgres.NewMembershipRepo(db)
	var ids []string
	for _, person := range []struct{ email, name string }{
		{"andreas@hearth.family", "Andreas"},
		{"christine@hearth.family", "Christine"},
	} {
		u, err := users.Create(ctx, person.email, "hash", person.name)
		if err != nil {
			t.Fatalf("create user %s: %v", person.name, err)
		}
		m, err := members.Create(ctx, domain.Membership{
			HouseholdID: h.ID, UserID: u.ID, Role: domain.RoleOwner,
			Capabilities: domain.AllCapabilities(),
		})
		if err != nil {
			t.Fatalf("create membership %s: %v", person.name, err)
		}
		ids = append(ids, m.ID)
	}
	return h.ID, ids[0], ids[1]
}

// Two owners, and each is about to take the OTHER's ownership away -- one by
// demoting, one by removing. Each change is legal on its own (the household
// keeps one owner) and illegal together (it keeps none). Without a lock, both
// checks read "two owners", both pass, both commit, and the household has
// nobody left who can manage it.
//
// UpdateWithCheck and DeleteWithCheck close that by locking the household,
// listing its memberships inside the same transaction, and running the
// caller's check on that list. The checks here are exactly what MemberService
// passes: the domain's own validators.
func TestADemotionAndARemovalRacingCannotLeaveAHouseholdWithoutAnOwner(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	members := postgres.NewMembershipRepo(db)
	householdID, andreasID, christineID := twoOwnerHousehold(t, db)

	// Each check sleeps before deciding, to hold the window between reading
	// the memberships and writing open long enough for the two calls to
	// genuinely overlap (docs/LEARNING.md pattern 19). A barrier at the start
	// alone lets two millisecond-long transactions serialise by luck, and the
	// test would then pass with no lock at all.
	//
	// With the lock: the second call blocks on it before it ever lists, and
	// then checks against the first call's committed result.
	// Without it: both list two owners, both sleep, both write.
	holdWindowOpen := func() { time.Sleep(300 * time.Millisecond) }
	limited := domain.Capabilities{domain.CapCalendar, domain.CapChores, domain.CapMoney}
	demoteAndreas := func() error {
		return members.UpdateWithCheck(ctx, householdID, andreasID,
			func(current []domain.Membership) (domain.Role, domain.Capabilities, error) {
				holdWindowOpen()
				err := domain.ValidateMembershipChange(current, andreasID, domain.RoleLimited, limited)
				return domain.RoleLimited, limited, err
			})
	}
	removeChristine := func() error {
		return members.DeleteWithCheck(ctx, householdID, christineID,
			func(current []domain.Membership) error {
				holdWindowOpen()
				return domain.ValidateMembershipRemoval(current, christineID)
			})
	}

	results := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for _, change := range []func() error{demoteAndreas, removeChristine} {
		go func() {
			start.Wait()
			results <- change()
		}()
	}
	start.Done()
	first, second := <-results, <-results

	refused := 0
	for _, err := range []error{first, second} {
		if errors.Is(err, domain.ErrLastOwner) {
			refused++
		} else if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if refused != 1 {
		t.Fatalf("%d of 2 racing ownership changes were refused, want exactly 1 (errors: %v, %v)", refused, first, second)
	}

	after, err := members.List(ctx, householdID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	owners := 0
	for _, v := range after {
		if v.Membership.Role == domain.RoleOwner {
			owners++
		}
	}
	if owners != 1 {
		t.Fatalf("household has %d owner(s) after the race, want exactly 1: %+v", owners, after)
	}
}

// The check receives the household's memberships and, when it refuses, the
// write does not happen and the check's own error comes back unchanged -- so
// MemberService can return domain.ErrLastOwner and the HTTP layer can map it.
func TestARefusedCheckWritesNothingAndReturnsTheCheckError(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	members := postgres.NewMembershipRepo(db)
	householdID, andreasID, christineID := twoOwnerHousehold(t, db)
	refusal := errors.New("refused by the check")

	var seen []domain.Membership
	refuse := func(current []domain.Membership) error {
		seen = current
		return refusal
	}

	refuseUpdate := func(current []domain.Membership) (domain.Role, domain.Capabilities, error) {
		return domain.RoleLimited, domain.Capabilities{domain.CapCalendar}, refuse(current)
	}
	if err := members.UpdateWithCheck(ctx, householdID, andreasID, refuseUpdate); !errors.Is(err, refusal) {
		t.Fatalf("UpdateWithCheck error = %v, want the check's own error", err)
	}
	if len(seen) != 2 {
		t.Fatalf("check saw %d memberships, want both of the household's 2", len(seen))
	}
	if err := members.DeleteWithCheck(ctx, householdID, christineID, refuse); !errors.Is(err, refusal) {
		t.Fatalf("DeleteWithCheck error = %v, want the check's own error", err)
	}

	after, err := members.List(ctx, householdID)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("len(after) = %d, want 2 -- a refused delete must delete nothing", len(after))
	}
	for _, v := range after {
		if v.Membership.Role != domain.RoleOwner {
			t.Fatalf("%s is %s, want owner -- a refused update must change nothing", v.User.DisplayName, v.Membership.Role)
		}
	}
}

// A membership that is not this household's is not found, whether it does not
// exist at all or belongs to another family -- the lock and the list are both
// scoped by household, so the other family's row is never even seen.
func TestCheckedWritesOnAnotherHouseholdsMembershipAreNotFound(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	members := postgres.NewMembershipRepo(db)
	_, andreasID, _ := twoOwnerHousehold(t, db)
	otherHousehold := insertTestHousehold(t, db)

	if err := members.UpdateWithCheck(ctx, otherHousehold, andreasID,
		write(domain.RoleLimited, domain.Capabilities{domain.CapCalendar})); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("UpdateWithCheck across households: error = %v, want ErrNotFound", err)
	}
	if err := members.DeleteWithCheck(ctx, otherHousehold, andreasID, allowAny); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("DeleteWithCheck across households: error = %v, want ErrNotFound", err)
	}
}
