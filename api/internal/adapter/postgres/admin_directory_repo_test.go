package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// directoryFixture is one household with an owner, built the way every
// other repository test here builds one. Extra members, sessions, invites
// and sign-ups are added per test, because each test's point is which of
// them the query sees.
type directoryFixture struct {
	db         *postgres.DB
	households *postgres.HouseholdRepo
	users      *postgres.UserRepo
	members    *postgres.MembershipRepo
	sessions   *postgres.SessionRepo
	invites    *postgres.InviteRepo
	signups    *postgres.SignupRepo
	dir        *postgres.AdminDirectoryRepo
}

func newDirectoryFixture(t *testing.T) *directoryFixture {
	t.Helper()
	db := openTestDB(t)
	return &directoryFixture{
		db:         db,
		households: postgres.NewHouseholdRepo(db),
		users:      postgres.NewUserRepo(db),
		members:    postgres.NewMembershipRepo(db),
		sessions:   postgres.NewSessionRepo(db),
		invites:    postgres.NewInviteRepo(db),
		signups:    postgres.NewSignupRepo(db),
		dir:        postgres.NewAdminDirectoryRepo(db),
	}
}

func (f *directoryFixture) household(t *testing.T, name, family string) domain.Household {
	t.Helper()
	h, err := f.households.Create(context.Background(), domain.Household{
		Name: name, FamilyName: family, PrimaryCurrency: "SGD", SecondaryCurrency: "IDR",
	})
	if err != nil {
		t.Fatalf("create household %q: %v", name, err)
	}
	return h
}

// member creates a user (email "" means a Telegram-only account: users.email
// is NULL) and their membership.
func (f *directoryFixture) member(t *testing.T, householdID, email, name string, role domain.Role, caps domain.Capabilities) domain.User {
	t.Helper()
	u, err := f.users.Create(context.Background(), email, "", name)
	if err != nil {
		t.Fatalf("create user %q: %v", name, err)
	}
	if _, err := f.members.Create(context.Background(), domain.Membership{
		HouseholdID: householdID, UserID: u.ID, Role: role, Capabilities: caps,
	}); err != nil {
		t.Fatalf("create membership for %q: %v", name, err)
	}
	return u
}

func (f *directoryFixture) linkTelegram(t *testing.T, userID string, chatID int64) {
	t.Helper()
	// There is deliberately no repository method for this outside the
	// sign-up transaction (SignupRepo.Provision); a test fixture writes the
	// row directly.
	if _, err := f.db.Pool().Exec(context.Background(),
		"INSERT INTO telegram_accounts (user_id, chat_id) VALUES ($1, $2)", userID, chatID); err != nil {
		t.Fatalf("link telegram: %v", err)
	}
}

// session creates a session and, when seen is non-zero, touches it. The
// token hash only has to be unique.
func (f *directoryFixture) session(t *testing.T, userID, householdID, token string, seen time.Time) []byte {
	t.Helper()
	hash := []byte(token + "-------------------------------")[:32]
	if err := f.sessions.Create(context.Background(), hash, userID, householdID, time.Now().Add(24*time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if !seen.IsZero() {
		if err := f.sessions.Touch(context.Background(), hash, seen); err != nil {
			t.Fatalf("touch session: %v", err)
		}
	}
	return hash
}

// backdateSession moves a session's created_at, which the repository never
// lets a caller set -- the "old session, recently touched" case needs it.
func (f *directoryFixture) backdateSession(t *testing.T, hash []byte, createdAt time.Time) {
	t.Helper()
	if _, err := f.db.Pool().Exec(context.Background(),
		"UPDATE sessions SET created_at = $1 WHERE token_hash = $2", createdAt, hash); err != nil {
		t.Fatalf("backdate session: %v", err)
	}
}

func TestSearchHouseholdsMatchesEveryFieldCaseInsensitively(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	now := time.Now()
	// The household's own name deliberately does not contain "christ" --
	// "Andreas & Christine" would, since "Christine" contains it as a
	// substring, which would make h.name ILIKE '%christ%' true and (per
	// ports.go's own contract on HouseholdListing.Match) suppress the
	// member match this case exists to prove.
	h := f.household(t, "Andreas & Kris", "Oentoro")
	f.member(t, h.ID, "christine@hearth.family", "Christine", domain.RoleOwner, domain.AllCapabilities())
	other := f.household(t, "Tan", "Tan")
	f.member(t, other.ID, "wei@example.test", "Wei", domain.RoleOwner, domain.AllCapabilities())

	cases := []struct {
		q         string
		wantID    string
		wantMatch string // "" when the household itself matched
	}{
		{"andreas", h.ID, ""},
		{"OENTORO", h.ID, ""},
		{"CHRISTINE@", h.ID, "Christine"},
		{"christ", h.ID, "Christine"},
		// "ris" hits both: the household's own name ("Kris") and a member's
		// display name ("Christine"). The household match must win -- Match
		// stays nil -- proving HouseholdMatched suppresses a member match
		// rather than the two being reported together.
		{"ris", h.ID, ""},
		{"wei@example", other.ID, "Wei"},
	}
	for _, tc := range cases {
		rows, err := f.dir.SearchHouseholds(ctx, tc.q, 10, now)
		if err != nil {
			t.Fatalf("SearchHouseholds(%q): %v", tc.q, err)
		}
		if len(rows) != 1 || rows[0].ID != tc.wantID {
			t.Fatalf("SearchHouseholds(%q) = %+v, want exactly household %s", tc.q, rows, tc.wantID)
		}
		switch {
		case tc.wantMatch == "" && rows[0].Match != nil:
			t.Fatalf("SearchHouseholds(%q): household matched by name but Match = %+v", tc.q, rows[0].Match)
		case tc.wantMatch != "" && (rows[0].Match == nil || rows[0].Match.Name != tc.wantMatch):
			t.Fatalf("SearchHouseholds(%q): Match = %+v, want member %q", tc.q, rows[0].Match, tc.wantMatch)
		}
	}
}

func TestSearchHouseholdsEmptyQueryReturnsEveryoneWithNoMatch(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	h := f.household(t, "A", "A")
	f.member(t, h.ID, "a@example.test", "Alpha", domain.RoleOwner, domain.AllCapabilities())
	f.household(t, "B", "B")

	rows, err := f.dir.SearchHouseholds(ctx, "", 10, time.Now())
	if err != nil {
		t.Fatalf("SearchHouseholds: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("empty query returned %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if r.Match != nil {
			t.Fatalf("empty query named a matched member on %q: %+v", r.Name, r.Match)
		}
	}
}

// An underscore is a LIKE wildcard; unescaped it matches every household.
func TestSearchHouseholdsEscapesLikeWildcards(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	f.household(t, "Plain", "Plain")
	f.household(t, "Under_score", "X")

	rows, err := f.dir.SearchHouseholds(ctx, "_", 10, time.Now())
	if err != nil {
		t.Fatalf("SearchHouseholds: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "Under_score" {
		t.Fatalf("searching for a literal underscore returned %+v", rows)
	}
}

func TestSearchHouseholdsOrdersMostRecentlyActiveFirstNeverActiveLast(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	stale := f.household(t, "Stale", "S")
	staleOwner := f.member(t, stale.ID, "s@example.test", "S", domain.RoleOwner, domain.AllCapabilities())
	f.session(t, staleOwner.ID, stale.ID, "stale", now.Add(-72*time.Hour))

	fresh := f.household(t, "Fresh", "F")
	freshOwner := f.member(t, fresh.ID, "f@example.test", "F", domain.RoleOwner, domain.AllCapabilities())
	f.session(t, freshOwner.ID, fresh.ID, "fresh", now.Add(-time.Hour))

	f.household(t, "Never", "N")

	rows, err := f.dir.SearchHouseholds(ctx, "", 10, now)
	if err != nil {
		t.Fatalf("SearchHouseholds: %v", err)
	}
	if len(rows) != 3 || rows[0].Name != "Fresh" || rows[1].Name != "Stale" || rows[2].Name != "Never" {
		names := make([]string, len(rows))
		for i, r := range rows {
			names[i] = r.Name
		}
		t.Fatalf("order = %v, want [Fresh Stale Never]", names)
	}
	if rows[2].LastActiveAt != nil {
		t.Fatalf("a household with no sessions reported LastActiveAt = %v", rows[2].LastActiveAt)
	}
	if rows[0].MemberCount != 1 {
		t.Fatalf("MemberCount = %d, want 1", rows[0].MemberCount)
	}
}

func TestMetricsCountsActiveByLastSeenNotCreatedAt(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	// Signed in 40 days ago, used yesterday: active.
	old := f.household(t, "Old", "O")
	oldOwner := f.member(t, old.ID, "o@example.test", "O", domain.RoleOwner, domain.AllCapabilities())
	hash := f.session(t, oldOwner.ID, old.ID, "old", now.Add(-24*time.Hour))
	f.backdateSession(t, hash, now.Add(-40*24*time.Hour))

	// Signed in 10 days ago, never touched: not active.
	gone := f.household(t, "Gone", "G")
	goneOwner := f.member(t, gone.ID, "g@example.test", "G", domain.RoleOwner, domain.AllCapabilities())
	goneHash := f.session(t, goneOwner.ID, gone.ID, "gone", time.Time{})
	f.backdateSession(t, goneHash, now.Add(-10*24*time.Hour))

	m, err := f.dir.Metrics(ctx, now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour), now)
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if m.Households != 2 {
		t.Fatalf("Households = %d, want 2", m.Households)
	}
	if m.ActiveHouseholds != 1 {
		t.Fatalf("ActiveHouseholds = %d, want 1 (touched yesterday counts; signed in 10 days ago does not)", m.ActiveHouseholds)
	}
}

func TestMetricsCountsSignupsAcrossBothChannelsAndInvitesStillPending(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	expires := now.Add(time.Hour)

	if err := f.signups.Create(ctx, "one@example.test", []byte("signup-hash-1-------------------"), expires); err != nil {
		t.Fatalf("signup 1: %v", err)
	}
	if err := f.signups.CreateConsumed(ctx, "two@example.test", []byte("signup-hash-2-------------------"), expires); err != nil {
		t.Fatalf("signup 2: %v", err)
	}
	if err := f.signups.CreateForTelegram(ctx, 4242, []byte("signup-hash-3-------------------"), expires); err != nil {
		t.Fatalf("signup 3: %v", err)
	}

	h := f.household(t, "H", "H")
	owner := f.member(t, h.ID, "owner@example.test", "Owner", domain.RoleOwner, domain.AllCapabilities())
	pending := func(email, hash string, expiresAt time.Time) string {
		id, err := f.invites.Create(ctx, h.ID, email, "Someone", domain.RoleLimited,
			domain.Capabilities{domain.CapCalendar}, []byte(hash), owner.ID, expiresAt)
		if err != nil {
			t.Fatalf("invite %s: %v", email, err)
		}
		return id
	}
	pending("p1@example.test", "invite-hash-1-------------------", expires)
	pending("p2@example.test", "invite-hash-2-------------------", now.Add(-time.Hour)) // expired
	accepted := pending("p3@example.test", "invite-hash-3-------------------", expires)
	if _, err := f.db.Pool().Exec(ctx, "UPDATE invites SET accepted_at = now() WHERE id = $1", accepted); err != nil {
		t.Fatalf("accept invite: %v", err)
	}

	m, err := f.dir.Metrics(ctx, now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour), now)
	if err != nil {
		t.Fatalf("Metrics: %v", err)
	}
	if m.SignupsRequested != 3 || m.SignupsCompleted != 1 {
		t.Fatalf("sign-ups = %d requested / %d completed, want 3 / 1", m.SignupsRequested, m.SignupsCompleted)
	}
	if m.PendingInvites != 1 {
		t.Fatalf("PendingInvites = %d, want 1 (expired and accepted excluded)", m.PendingInvites)
	}
}

func TestHouseholdDetailNamesTheChannelFromTheTelegramJoin(t *testing.T) {
	f := newDirectoryFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	h := f.household(t, "H", "H")
	owner := f.member(t, h.ID, "owner@example.test", "Owner", domain.RoleOwner, domain.AllCapabilities())
	kid := f.member(t, h.ID, "", "Kid", domain.RoleLimited, domain.Capabilities{domain.CapCalendar})
	f.linkTelegram(t, kid.ID, 777)
	if _, err := f.invites.Create(ctx, h.ID, "c@example.test", "Christine", domain.RoleOwner,
		domain.AllCapabilities(), []byte("invite-hash-c-------------------"), owner.ID, now.Add(time.Hour)); err != nil {
		t.Fatalf("invite: %v", err)
	}

	detail, err := f.dir.Household(ctx, h.ID, now)
	if err != nil {
		t.Fatalf("Household: %v", err)
	}
	if detail.Name != "H" || len(detail.Members) != 2 {
		t.Fatalf("detail = %+v", detail)
	}
	byName := map[string]usecase.HouseholdMember{}
	for _, m := range detail.Members {
		byName[m.Name] = m
	}
	if o := byName["Owner"]; o.Channel != usecase.ChannelEmail || o.Email == nil || *o.Email != "owner@example.test" || o.Role != domain.RoleOwner {
		t.Fatalf("owner = %+v", o)
	}
	if k := byName["Kid"]; k.Channel != usecase.ChannelTelegram || k.Email != nil || k.Role != domain.RoleLimited || !k.Capabilities.Has(domain.CapCalendar) {
		t.Fatalf("kid = %+v", k)
	}
	if byName["Kid"].LastActiveAt != nil {
		t.Fatalf("a member with no session reported LastActiveAt = %v", byName["Kid"].LastActiveAt)
	}
	if len(detail.PendingInvites) != 1 || detail.PendingInvites[0].InvitedByName != "Owner" || detail.PendingInvites[0].Email != "c@example.test" {
		t.Fatalf("pending invites = %+v", detail.PendingInvites)
	}

	// A Telegram-only member is found by name, and the listing's match has
	// no email to show.
	rows, err := f.dir.SearchHouseholds(ctx, "kid", 10, now)
	if err != nil {
		t.Fatalf("SearchHouseholds: %v", err)
	}
	if len(rows) != 1 || rows[0].Match == nil || rows[0].Match.Name != "Kid" || rows[0].Match.Email != nil {
		t.Fatalf("search by a Telegram-only member's name = %+v", rows)
	}
}

func TestHouseholdDetailUnknownIsNotFound(t *testing.T) {
	f := newDirectoryFixture(t)
	_, err := f.dir.Household(context.Background(), "00000000-0000-0000-0000-000000000000", time.Now())
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
