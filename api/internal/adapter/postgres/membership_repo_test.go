package postgres_test

import (
	"context"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// TestMembershipRepoRoundTrip exercises List, ByUser, Update and Delete,
// none of which repos_test.go's TestMembershipRepoRejectsAnInvalidCapabilitySet
// reaches (that test only exercises Create's error path). List in particular
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

	if err := members.Update(ctx, h.ID, created.ID, domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	byUser, err = members.ByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("ByUser after update: %v", err)
	}
	if byUser.Role != domain.RoleLimited || len(byUser.Capabilities) != 1 || byUser.Capabilities[0] != domain.CapCalendar {
		t.Fatalf("after update: %+v", byUser)
	}

	if err := members.Delete(ctx, h.ID, created.ID); err != nil {
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
