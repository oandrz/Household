package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// waysIn is one of every credential a person can hold, so a test can check
// each one after RevokeAccess.
type waysIn struct {
	userID        string
	sessionHash   []byte
	adminSession  []byte
	tokenHash     []byte
	magicLinkHash []byte
	openLinkID    string // a link request nobody has redeemed yet
	pendingLinkID string // redeemed by a chat, waiting for the browser's confirm
	chatID        int64
}

type revokeRepos struct {
	users    *postgres.UserRepo
	sessions *postgres.SessionRepo
	tokens   *postgres.APITokenRepo
	magic    *postgres.MagicLinkRepo
	links    *postgres.TelegramLinkRepo
	chats    *postgres.TelegramAccountRepo
	revoker  *postgres.AccessRevokeRepo
	pool     *pgxpool.Pool
}

func newRevokeRepos(db *postgres.DB) revokeRepos {
	return revokeRepos{
		users: postgres.NewUserRepo(db), sessions: postgres.NewSessionRepo(db),
		tokens: postgres.NewAPITokenRepo(db), magic: postgres.NewMagicLinkRepo(db),
		links: postgres.NewTelegramLinkRepo(db), chats: postgres.NewTelegramAccountRepo(db),
		revoker: postgres.NewAccessRevokeRepo(db), pool: db.Pool(),
	}
}

// giveEveryWayIn creates a user holding one of each credential. prefix keeps
// two users' secrets apart.
func giveEveryWayIn(t *testing.T, r revokeRepos, householdID, email, prefix string, chatID int64) waysIn {
	t.Helper()
	ctx := context.Background()
	later := time.Now().Add(time.Hour)
	u, err := r.users.Create(ctx, email, "$argon2id$fake", prefix)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	w := waysIn{
		userID: u.ID, sessionHash: []byte(prefix + "-session"), adminSession: []byte(prefix + "-admin"),
		tokenHash: []byte(prefix + "-token"), magicLinkHash: []byte(prefix + "-magic"), chatID: chatID,
	}
	for _, h := range [][]byte{w.sessionHash, w.adminSession} {
		if err := r.sessions.Create(ctx, h, u.ID, householdID, later); err != nil {
			t.Fatalf("create session: %v", err)
		}
	}
	if err := r.sessions.GrantAdmin(ctx, w.adminSession, &later); err != nil {
		t.Fatalf("grant admin: %v", err)
	}
	if _, err := r.tokens.Create(ctx, w.tokenHash, prefix+"-tk", domain.APIToken{
		UserID: u.ID, HouseholdID: householdID, Name: "script", ExpiresAt: later,
	}); err != nil {
		t.Fatalf("create token: %v", err)
	}
	if err := r.magic.Create(ctx, u.ID, w.magicLinkHash, later); err != nil {
		t.Fatalf("create magic link: %v", err)
	}
	if w.openLinkID, err = r.links.Create(ctx, u.ID, []byte(prefix+"-open-nonce"), later); err != nil {
		t.Fatalf("create open link request: %v", err)
	}
	if w.pendingLinkID, err = r.links.Create(ctx, u.ID, []byte(prefix+"-pending-nonce"), later); err != nil {
		t.Fatalf("create pending link request: %v", err)
	}
	if _, err := r.links.Consume(ctx, []byte(prefix+"-pending-nonce"), chatID+1, prefix+"_new_chat"); err != nil {
		t.Fatalf("redeem pending link request: %v", err)
	}
	if err := r.chats.Create(ctx, usecase.TelegramBinding{UserID: u.ID, ChatID: chatID, ChatUsername: prefix}); err != nil {
		t.Fatalf("bind chat: %v", err)
	}
	return w
}

func revokeTestHousehold(t *testing.T, db *postgres.DB) string {
	t.Helper()
	h, err := postgres.NewHouseholdRepo(db).Create(context.Background(), domain.Household{
		Timezone: "UTC", Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR",
	})
	if err != nil {
		t.Fatalf("create household: %v", err)
	}
	return h.ID
}

// assertNoWayIn checks every credential through the read path that would
// let it in, not by peeking at columns.
func assertNoWayIn(t *testing.T, r revokeRepos, w waysIn, wantChat bool) {
	t.Helper()
	ctx := context.Background()
	for _, h := range [][]byte{w.sessionHash, w.adminSession} {
		if _, err := r.sessions.ByTokenHash(ctx, h); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("session %s still live: %v", h, err)
		}
	}
	if _, err := r.tokens.ByTokenHash(ctx, w.tokenHash); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("API token still live: %v", err)
	}
	if _, err := r.magic.Consume(ctx, w.magicLinkHash); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("magic link still usable: %v", err)
	}
	// The database's clock, not this process's: expires_at is set from
	// Postgres's now(), and the container's clock can run ahead of the host's.
	var dbNow time.Time
	if err := r.pool.QueryRow(ctx, `SELECT now()`).Scan(&dbNow); err != nil {
		t.Fatalf("read the database clock: %v", err)
	}
	for _, id := range []string{w.openLinkID, w.pendingLinkID} {
		row, err := r.links.ByID(ctx, id)
		if err != nil {
			t.Fatalf("read link request: %v", err)
		}
		if row.ExpiresAt.After(dbNow) {
			t.Errorf("link request %s still open until %v", id, row.ExpiresAt)
		}
	}
	_, err := r.chats.ByUserID(ctx, w.userID)
	if wantChat && err != nil {
		t.Errorf("chat binding gone, want it kept: %v", err)
	}
	if !wantChat && !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("chat binding still there: %v", err)
	}
}

func TestRevokeAccessEndsEveryWayInForThatPersonOnly(t *testing.T) {
	db := openTestDB(t)
	r := newRevokeRepos(db)
	hh := revokeTestHousehold(t, db)
	andreas := giveEveryWayIn(t, r, hh, "andreas@hearth.family", "andreas", 1001)
	christine := giveEveryWayIn(t, r, hh, "christine@hearth.family", "christine", 2001)

	if err := r.revoker.RevokeAccess(context.Background(), andreas.userID, usecase.RevokeChat); err != nil {
		t.Fatalf("RevokeAccess: %v", err)
	}

	assertNoWayIn(t, r, andreas, false)

	ctx := context.Background()
	if _, err := r.sessions.ByTokenHash(ctx, christine.sessionHash); err != nil {
		t.Errorf("the other person's session was ended: %v", err)
	}
	if _, err := r.tokens.ByTokenHash(ctx, christine.tokenHash); err != nil {
		t.Errorf("the other person's token was revoked: %v", err)
	}
	if _, err := r.chats.ByUserID(ctx, christine.userID); err != nil {
		t.Errorf("the other person's chat was unlinked: %v", err)
	}
}

func TestRevokeAccessWithKeepChatLeavesTheBinding(t *testing.T) {
	db := openTestDB(t)
	r := newRevokeRepos(db)
	w := giveEveryWayIn(t, r, revokeTestHousehold(t, db), "kayla@hearth.family", "kayla", 3001)

	if err := r.revoker.RevokeAccess(context.Background(), w.userID, usecase.KeepChat); err != nil {
		t.Fatalf("RevokeAccess: %v", err)
	}
	assertNoWayIn(t, r, w, true)
}

func TestRevokeAccessRefusesAChatRuleNobodyChoseAndRevokesNothing(t *testing.T) {
	db := openTestDB(t)
	r := newRevokeRepos(db)
	w := giveEveryWayIn(t, r, revokeTestHousehold(t, db), "ethan@hearth.family", "ethan", 4001)

	var forgotten usecase.ChatRule
	if err := r.revoker.RevokeAccess(context.Background(), w.userID, forgotten); err == nil {
		t.Fatal("RevokeAccess with the zero ChatRule = nil, want a refusal")
	}
	if _, err := r.sessions.ByTokenHash(context.Background(), w.sessionHash); err != nil {
		t.Fatalf("a refused call ended a session: %v", err)
	}
}

// TestEveryWayInIsCoveredByRevokeAccess is what catches the next kind of
// credential. It lists every table that names a person (a foreign key to
// users) or stores a secret's hash (a column ending _hash), and requires each
// one to be classified below: either RevokeAccess ends it, or the reason it
// is not a way in is written down.
//
// Its blind spot: a credential stored without either shape -- keyed by
// something other than a users foreign key, holding its secret in a column
// not named *_hash -- is not seen. Give a new credential one of the two
// shapes, or widen the query.
func TestEveryWayInIsCoveredByRevokeAccess(t *testing.T) {
	db := openTestDB(t)
	classified := map[string]string{
		"sessions":                "ended by RevokeAccess",
		"api_tokens":              "ended by RevokeAccess",
		"magic_links":             "ended by RevokeAccess (marked used)",
		"telegram_link_requests":  "ended by RevokeAccess (expired)",
		"telegram_accounts":       "ended by RevokeAccess unless KeepChat",
		"users":                   "holds the password, which the caller changes before RevokeAccess",
		"memberships":             "what a credential is checked against, not a credential",
		"invites":                 "the invited person's way in, withdrawn by an owner",
		"signups":                 "keyed by an address or a chat before any account exists",
		"login_attempts":          "a counter",
		"admin_reauth_attempts":   "a counter",
		"platform_admins":         "a privilege that still needs a live session",
		"feature_flags":           "records who changed a flag",
		"household_feature_flags": "records who changed a flag",
		"admin_audit_log":         "a record of what happened",
	}

	rows, err := db.Pool().Query(context.Background(), `
		SELECT conrelid::regclass::text FROM pg_constraint
		WHERE contype = 'f' AND confrelid = 'users'::regclass
		UNION
		SELECT table_name::text FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name LIKE '%\_hash'`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	defer rows.Close()
	found := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		found[name] = true
		if _, ok := classified[name]; !ok {
			t.Errorf("table %q names a person or stores a secret's hash, but nothing says whether "+
				"RevokeAccess covers it -- end it in AccessRevokeRepo.RevokeAccess, or list it here "+
				"with the reason it is not a way in", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	for name := range classified {
		if !found[name] {
			t.Errorf("%q is listed but no longer names a person or holds a hash -- remove it", name)
		}
	}
}
