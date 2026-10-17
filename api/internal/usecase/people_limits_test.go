package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// peopleCase is one way a household gains a member or a pending invite.
// setup runs before the count is set, so it can create whatever invite the
// action needs; act is the write under test.
type peopleCase struct {
	kind  domain.LimitKind
	limit int
	setup func(t *testing.T, f *fixture) string
	act   func(t *testing.T, f *fixture, prepared string) error
}

func peopleCases() map[string]peopleCase {
	ctx := context.Background()
	emailInvite := func(t *testing.T, f *fixture) string {
		t.Helper()
		if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
			domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
			t.Fatalf("Create: %v", err)
		}
		return lastInviteToken(t, f)
	}
	knockedInvite := func(t *testing.T, f *fixture) string {
		t.Helper()
		invite, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, "Christine", domain.RoleOwner, domain.AllCapabilities())
		if err != nil {
			t.Fatalf("CreateTelegram: %v", err)
		}
		if _, err := f.invites.Knock(ctx, telegramRawToken(t, invite.URL), 4242, "christine_t"); err != nil {
			t.Fatalf("Knock: %v", err)
		}
		return invite.ID
	}
	nothing := func(*testing.T, *fixture) string { return "" }

	return map[string]peopleCase{
		"an email invite": {domain.LimitPendingInvites, domain.MaxPendingInvites, nothing,
			func(t *testing.T, f *fixture, _ string) error {
				return f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
					domain.RoleLimited, domain.Capabilities{domain.CapCalendar})
			}},
		"a Telegram invite": {domain.LimitPendingInvites, domain.MaxPendingInvites, nothing,
			func(t *testing.T, f *fixture, _ string) error {
				_, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, "Christine", domain.RoleOwner, domain.AllCapabilities())
				return err
			}},
		"a member without an email": {domain.LimitMembers, domain.MaxMembers, nothing,
			func(t *testing.T, f *fixture, _ string) error {
				return f.invites.Create(ctx, f.householdID, f.andreasID, "Little one", "",
					domain.RoleLimited, domain.Capabilities{domain.CapCalendar})
			}},
		"accepting an invite": {domain.LimitMembers, domain.MaxMembers, emailInvite,
			func(t *testing.T, f *fixture, token string) error {
				_, err := f.invites.Accept(ctx, token, "supersecretpassword", "Kid")
				return err
			}},
		"admitting a knock": {domain.LimitMembers, domain.MaxMembers, knockedInvite,
			func(t *testing.T, f *fixture, inviteID string) error {
				_, err := f.invites.Admit(ctx, f.householdID, inviteID)
				return err
			}},
	}
}

func TestMembersAndPendingInvitesAreCapped(t *testing.T) {
	for name, c := range peopleCases() {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			prepared := c.setup(t, f)
			f.inviteUsage.set(c.kind, c.limit-1)
			if err := c.act(t, f, prepared); err != nil {
				t.Fatalf("one below the cap: %v", err)
			}

			f = newFixture(t)
			prepared = c.setup(t, f)
			f.inviteUsage.set(c.kind, c.limit)
			err := c.act(t, f, prepared)
			var limit *domain.LimitReachedError
			if !errors.As(err, &limit) || limit.Kind != c.kind {
				t.Fatalf("at the cap: err = %v, want the %s cap", err, c.kind)
			}
		})
	}
}

func TestActiveAPITokensAreCapped(t *testing.T) {
	in := usecase.NewAPITokenInput{UserID: "u1", HouseholdID: "h1", Name: "laptop"}
	below := newUsageDouble()
	below.set(domain.LimitActiveAPITokens, domain.MaxActiveAPITokens-1)
	if _, err := apiTokenServiceWithUsage(t, below).Create(context.Background(), in); err != nil {
		t.Fatalf("one below the cap: %v", err)
	}
	at := newUsageDouble()
	at.set(domain.LimitActiveAPITokens, domain.MaxActiveAPITokens)
	if _, err := apiTokenServiceWithUsage(t, at).Create(context.Background(), in); !errors.Is(err, domain.ErrLimitReached) {
		t.Fatalf("at the cap: err = %v, want ErrLimitReached", err)
	}
}

// Every name a person types about people or the household is limited to 80
// characters.
func TestPeopleAndHouseholdNamesOverEightyCharactersAreRefused(t *testing.T) {
	ctx := context.Background()
	long := strings.Repeat("a", domain.MaxNameLen+1)
	refused := func(t *testing.T, err error) {
		t.Helper()
		if !errors.Is(err, domain.ErrTextTooLong) {
			t.Fatalf("err = %v, want ErrTextTooLong", err)
		}
	}

	t.Run("an email invitee's name", func(t *testing.T) {
		f := newFixture(t)
		refused(t, f.invites.Create(ctx, f.householdID, f.andreasID, long, "kid@example.com",
			domain.RoleLimited, domain.Capabilities{domain.CapCalendar}))
	})
	t.Run("a Telegram invitee's name", func(t *testing.T) {
		f := newFixture(t)
		_, err := f.invites.CreateTelegram(ctx, f.householdID, f.andreasID, long, domain.RoleOwner, domain.AllCapabilities())
		refused(t, err)
	})
	t.Run("the display name chosen when accepting", func(t *testing.T) {
		f := newFixture(t)
		if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Kid", "kid@example.com",
			domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
			t.Fatal(err)
		}
		_, err := f.invites.Accept(ctx, lastInviteToken(t, f), "supersecretpassword", long)
		refused(t, err)
	})
	t.Run("the household name at sign-up", func(t *testing.T) {
		_, err := usecase.NewSignupBlueprint(long, "Andreas", "SGD", "Asia/Singapore")
		refused(t, err)
	})
	t.Run("the owner's display name at sign-up", func(t *testing.T) {
		_, err := usecase.NewSignupBlueprint("The Oentoros", long, "SGD", "Asia/Singapore")
		refused(t, err)
	})
	t.Run("renaming the household", func(t *testing.T) {
		f := newFixture(t)
		h, err := f.householdSvc.Get(ctx, f.householdID)
		if err != nil {
			t.Fatal(err)
		}
		h.Name = long
		_, err = f.householdSvc.Update(ctx, h)
		refused(t, err)
	})
	t.Run("renaming the family", func(t *testing.T) {
		f := newFixture(t)
		h, err := f.householdSvc.Get(ctx, f.householdID)
		if err != nil {
			t.Fatal(err)
		}
		h.FamilyName = long
		_, err = f.householdSvc.Update(ctx, h)
		refused(t, err)
	})
}
