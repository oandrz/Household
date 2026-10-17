package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// TestPruneMagicLinksDeletesOnlyLinksThatLapsedBeforeTheCutoff: a link that
// expired before the cutoff goes whether or not it was used; a link still
// inside its fifteen minutes stays, and so does one that lapsed after the
// cutoff.
func TestPruneMagicLinksDeletesOnlyLinksThatLapsedBeforeTheCutoff(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	links := postgres.NewMagicLinkRepo(db)
	u, err := postgres.NewUserRepo(db).Create(ctx, "andreas@hearth.family", "", "Andreas")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	now := time.Now()
	cutoff := now.Add(-usecase.RetentionWindow)
	for _, link := range []struct {
		hash    string
		expires time.Time
		consume bool
	}{
		{"old-unused-link-hash-0000000000001", cutoff.Add(-time.Hour), false},
		{"old-used-link-hash-00000000000002", cutoff.Add(-time.Hour), true},
		{"recent-lapsed-link-hash-000000003", cutoff.Add(time.Hour), false},
		{"live-link-hash-0000000000000000004", now.Add(15 * time.Minute), false},
	} {
		if err := links.Create(ctx, u.ID, []byte(link.hash), link.expires); err != nil {
			t.Fatalf("Create %s: %v", link.hash, err)
		}
		if link.consume {
			if _, err := db.Pool().Exec(ctx, `UPDATE magic_links SET consumed_at = expires_at - interval '1 minute' WHERE token_hash = $1`, []byte(link.hash)); err != nil {
				t.Fatalf("mark used: %v", err)
			}
		}
	}

	n, err := links.Prune(ctx, cutoff)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 2 {
		t.Fatalf("Prune deleted %d links, want 2 (the two that lapsed before the cutoff)", n)
	}
	var left int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM magic_links`).Scan(&left); err != nil {
		t.Fatalf("count: %v", err)
	}
	if left != 2 {
		t.Fatalf("links left = %d, want 2", left)
	}
	if _, err := links.Consume(ctx, []byte("live-link-hash-0000000000000000004")); err != nil {
		t.Fatalf("the live link no longer works after a prune: %v", err)
	}
}

// TestPruneExpiredInvitesNeverDeletesAnAcceptedInvite: only invites that
// nobody accepted and that lapsed before the cutoff go. An accepted invite is
// history, however old; a pending one stays.
func TestPruneExpiredInvitesNeverDeletesAnAcceptedInvite(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	invites := postgres.NewInviteRepo(db)
	h := createHouseholdForInviteTest(t, postgres.NewHouseholdRepo(db), "Oentoro")
	inviter, err := postgres.NewUserRepo(db).Create(ctx, "andreas@hearth.family", "hash", "Andreas")
	if err != nil {
		t.Fatalf("create inviter: %v", err)
	}
	now := time.Now()
	cutoff := now.Add(-usecase.RetentionWindow)

	create := func(email, hash string) string {
		t.Helper()
		id, err := invites.Create(ctx, h.ID, email, email, domain.RoleLimited,
			domain.Capabilities{domain.CapCalendar}, []byte(hash), inviter.ID, now.Add(72*time.Hour))
		if err != nil {
			t.Fatalf("Create %s: %v", email, err)
		}
		return id
	}
	expireAt := func(id string, at time.Time) {
		t.Helper()
		if _, err := db.Pool().Exec(ctx, `UPDATE invites SET expires_at = $2 WHERE id = $1`, id, at); err != nil {
			t.Fatalf("backdate: %v", err)
		}
	}

	oldUnaccepted := create("old@example.com", "prune-old-unaccepted-hash-000001")
	expireAt(oldUnaccepted, cutoff.Add(-time.Hour))

	oldAccepted := create("accepted@example.com", "prune-old-accepted-hash-0000002")
	if err := invites.MarkAccepted(ctx, oldAccepted); err != nil {
		t.Fatalf("MarkAccepted: %v", err)
	}
	expireAt(oldAccepted, cutoff.Add(-time.Hour))

	recentLapsed := create("recent@example.com", "prune-recent-lapsed-hash-000003")
	expireAt(recentLapsed, cutoff.Add(time.Hour))

	pending := create("pending@example.com", "prune-pending-hash-0000000000004")

	n, err := invites.Prune(ctx, cutoff)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("Prune deleted %d invites, want 1 (the old one nobody accepted)", n)
	}
	rows, err := db.Pool().Query(ctx, `SELECT id::text FROM invites`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	left := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		left[id] = true
	}
	for name, id := range map[string]string{"accepted": oldAccepted, "recently lapsed": recentLapsed, "pending": pending} {
		if !left[id] {
			t.Errorf("the %s invite was deleted", name)
		}
	}
	if left[oldUnaccepted] {
		t.Error("the old unaccepted invite is still there")
	}
}

// TestEarlierFailureListsKeepOnlyTheNewest: a household, an address or an
// operator with many failures on record costs the same read as one with a
// few, and what comes back is the newest -- the latest time decides when a
// lock ends.
func TestEarlierFailureListsKeepOnlyTheNewest(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	attempts := postgres.NewLoginAttemptRepo(db)
	admin := postgres.NewAdminReauthAttemptRepo(db)
	h := createHouseholdForAttemptTest(t, db, "H")
	operator, err := postgres.NewUserRepo(db).Create(ctx, "operator@example.test", "", "Operator")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	since := now.Add(-time.Hour)
	const onRecord = usecase.MaxEarlierFailures + 5
	newest := now.Add(-time.Minute)
	for i := 0; i < onRecord; i++ {
		// i == 0 is the newest; the rest step back a minute each.
		at := newest.Add(-time.Duration(i) * time.Minute)
		if err := attempts.Record(ctx, &h.ID, nil, "a@b.c", false, at); err != nil {
			t.Fatalf("Record (household): %v", err)
		}
		if err := attempts.Record(ctx, nil, nil, "stranger@example.com", false, at); err != nil {
			t.Fatalf("Record (address): %v", err)
		}
		if err := admin.Record(ctx, operator.ID, false, at); err != nil {
			t.Fatalf("Record (admin): %v", err)
		}
	}

	check := func(name string, got []time.Time, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(got) != usecase.MaxEarlierFailures {
			t.Fatalf("%s returned %d failures, want the newest %d of %d", name, len(got),
				usecase.MaxEarlierFailures, onRecord)
		}
		latest := got[0]
		for _, at := range got {
			if at.After(latest) {
				latest = at
			}
		}
		if !latest.Equal(newest) {
			t.Fatalf("%s: latest failure returned = %v, want the newest on record, %v", name, latest, newest)
		}
	}

	failures, err := attempts.FailuresSince(ctx, h.ID, since)
	check("FailuresSince", failures, err)
	r, err := attempts.ReserveFailure(ctx, &h.ID, nil, "a@b.c", now, since)
	check("ReserveFailure for a household", r.Earlier, err)
	r, err = attempts.ReserveFailure(ctx, nil, nil, "stranger@example.com", now, since)
	check("ReserveFailure for an address", r.Earlier, err)
	r, err = admin.ReserveFailure(ctx, operator.ID, now, since)
	check("ReserveFailure for an operator", r.Earlier, err)
}

// A token or session that was revoked, or expired, before the cutoff goes;
// a live one stays, and so does one revoked or expired after the cutoff.
func TestPruneTokensAndSessionsDeletesOnlyThoseSpentBeforeTheCutoff(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	household := insertTestHousehold(t, db)
	u, err := postgres.NewUserRepo(db).Create(ctx, "a@example.com", "", "A")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	cutoff := now.Add(-usecase.RetentionWindow)
	old := cutoff.Add(-time.Hour)
	recent := cutoff.Add(time.Hour)
	future := now.Add(24 * time.Hour)

	rows := []struct {
		name    string
		expires time.Time
		revoked *time.Time
	}{
		{"live", future, nil},
		{"expired before the cutoff", old, nil},
		{"revoked before the cutoff", future, &old},
		{"expired after the cutoff", recent, nil},
		{"revoked after the cutoff", future, &recent},
	}
	for i, r := range rows {
		if _, err := db.Pool().Exec(ctx,
			`INSERT INTO api_tokens (user_id, household_id, name, token_hash, prefix, expires_at, revoked_at)
			 VALUES ($1, $2, $3, $4, 'p', $5, $6)`, u.ID, household, r.name, []byte{byte(i)}, r.expires, r.revoked); err != nil {
			t.Fatalf("token %s: %v", r.name, err)
		}
		if _, err := db.Pool().Exec(ctx,
			`INSERT INTO sessions (token_hash, user_id, household_id, expires_at, revoked_at)
			 VALUES ($1, $2, $3, $4, $5)`, []byte{byte(100 + i)}, u.ID, household, r.expires, r.revoked); err != nil {
			t.Fatalf("session %s: %v", r.name, err)
		}
	}
	if n, err := postgres.NewAPITokenRepo(db).Prune(ctx, cutoff); err != nil || n != 2 {
		t.Fatalf("tokens pruned = %d, %v; want 2", n, err)
	}
	if n, err := postgres.NewSessionRepo(db).Prune(ctx, cutoff); err != nil || n != 2 {
		t.Fatalf("sessions pruned = %d, %v; want 2", n, err)
	}
	var tokens, sessions int
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM api_tokens`).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool().QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if tokens != 3 || sessions != 3 {
		t.Fatalf("left %d tokens and %d sessions, want 3 of each", tokens, sessions)
	}
}
