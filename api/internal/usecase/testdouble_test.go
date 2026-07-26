package usecase_test

import (
	"context"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// --- Clock, hasher, token generator -----------------------------------

type fixedClock struct{ now time.Time }

func (c *fixedClock) Now() time.Time          { return c.now }
func (c *fixedClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

// fakeHasher logs every Verify call, by encoded argument (via a pointer
// receiver, unlike the brief's value-receiver sketch), so a test can tell
// apart a real verification against a member's own stored hash from
// AuthService's timing-parity decoy verification, which runs against a
// different, unrelated encoded hash. verifyCallCount answers "did the
// hasher get touched at all" (the decoy's whole reason to exist); the log
// itself answers "was it touched with *this* encoded value" (the guard
// TestUsersWithoutAPasswordCannotSignIn cares about — a credential-less
// member's own empty PasswordHash must never reach Verify, decoy or not).
type fakeHasher struct {
	verifyLog []string /* encoded arguments */
}

func (h *fakeHasher) Hash(plain string) (string, error) { return "hashed:" + plain, nil }
func (h *fakeHasher) Verify(plain, encoded string) bool {
	h.verifyLog = append(h.verifyLog, encoded)
	return encoded == "hashed:"+plain
}

func (h *fakeHasher) verifyCallCount() int { return len(h.verifyLog) }

// verifyCallsWithEncoded counts Verify calls made against a specific encoded
// hash — e.g. a member's real (possibly empty) PasswordHash — independent of
// however many additional decoy calls, against a different encoded hash,
// also happened.
func (h *fakeHasher) verifyCallsWithEncoded(encoded string) int {
	n := 0
	for _, e := range h.verifyLog {
		if e == encoded {
			n++
		}
	}
	return n
}

type seqTokens struct{ n int }

func (t *seqTokens) NewToken() (string, []byte, error) {
	t.n++
	raw := fmt.Sprintf("token-%d", t.n)
	return raw, t.HashToken(raw), nil
}
func (t *seqTokens) HashToken(raw string) []byte { return []byte("hash:" + raw) }

// --- UserRepository ------------------------------------------------------

type userDouble struct {
	byID    map[string]usecase.StoredUser
	byEmail map[string]string // email -> id
	n       int
}

func newUserDouble() *userDouble {
	return &userDouble{byID: map[string]usecase.StoredUser{}, byEmail: map[string]string{}}
}

func (d *userDouble) put(u usecase.StoredUser) {
	d.byID[u.ID] = u
	if u.Email != "" {
		d.byEmail[u.Email] = u.ID
	}
}

func (d *userDouble) ByEmail(_ context.Context, email string) (usecase.StoredUser, error) {
	id, ok := d.byEmail[email]
	if !ok {
		return usecase.StoredUser{}, domain.ErrNotFound
	}
	return d.byID[id], nil
}

func (d *userDouble) ByID(_ context.Context, id string) (usecase.StoredUser, error) {
	u, ok := d.byID[id]
	if !ok {
		return usecase.StoredUser{}, domain.ErrNotFound
	}
	return u, nil
}

func (d *userDouble) Create(_ context.Context, email, passwordHash, displayName string) (domain.User, error) {
	d.n++
	u := usecase.StoredUser{
		User:         domain.User{ID: fmt.Sprintf("user-%d", d.n), Email: email, DisplayName: displayName},
		PasswordHash: passwordHash,
	}
	d.put(u)
	return u.User, nil
}

func (d *userDouble) SetPasswordHash(_ context.Context, userID, hash string) error {
	u, ok := d.byID[userID]
	if !ok {
		// SetPasswordHash is :exec in the real repository — an unknown id is a
		// silent no-op there, not an error.
		return nil
	}
	u.PasswordHash = hash
	d.byID[userID] = u
	return nil
}

// --- MembershipRepository --------------------------------------------

type membershipDouble struct {
	users  *userDouble
	byID   map[string]domain.Membership
	byUser map[string]string // userID -> membershipID; the real query is LIMIT 1
	n      int
}

func newMembershipDouble(users *userDouble) *membershipDouble {
	return &membershipDouble{users: users, byID: map[string]domain.Membership{}, byUser: map[string]string{}}
}

func (d *membershipDouble) put(m domain.Membership) {
	d.byID[m.ID] = m
	d.byUser[m.UserID] = m.ID
}

func (d *membershipDouble) List(_ context.Context, householdID string) ([]usecase.MemberView, error) {
	var out []usecase.MemberView
	for _, m := range d.byID {
		if m.HouseholdID != householdID {
			continue
		}
		u := d.users.byID[m.UserID]
		out = append(out, usecase.MemberView{Membership: m, User: u.User})
	}
	return out, nil
}

func (d *membershipDouble) ByUser(_ context.Context, userID string) (domain.Membership, error) {
	id, ok := d.byUser[userID]
	if !ok {
		return domain.Membership{}, domain.ErrNotFound
	}
	return d.byID[id], nil
}

func (d *membershipDouble) Create(_ context.Context, m domain.Membership) (domain.Membership, error) {
	d.n++
	m.ID = fmt.Sprintf("membership-%d", d.n)
	d.put(m)
	return m, nil
}

func (d *membershipDouble) Update(_ context.Context, householdID, membershipID string, role domain.Role, caps domain.Capabilities) error {
	m, ok := d.byID[membershipID]
	if !ok || m.HouseholdID != householdID {
		return nil // UpdateMembership is :exec — an unmatched row is a silent no-op.
	}
	m.Role = role
	m.Capabilities = caps
	d.byID[membershipID] = m
	return nil
}

func (d *membershipDouble) Delete(_ context.Context, householdID, membershipID string) error {
	m, ok := d.byID[membershipID]
	if !ok || m.HouseholdID != householdID {
		return nil // DeleteMembership is :exec — an unmatched row is a silent no-op.
	}
	delete(d.byID, membershipID)
	if d.byUser[m.UserID] == membershipID {
		delete(d.byUser, m.UserID)
	}
	return nil
}

// --- SessionRepository ----------------------------------------------

type sessionRow struct {
	UserID      string
	HouseholdID string
	ExpiresAt   time.Time
	Revoked     bool
}

type sessionDouble struct {
	clock   *fixedClock
	rows    map[string]*sessionRow // keyed by string(tokenHash)
	created int
}

func newSessionDouble(clock *fixedClock) *sessionDouble {
	return &sessionDouble{clock: clock, rows: map[string]*sessionRow{}}
}

func (d *sessionDouble) Create(_ context.Context, tokenHash []byte, userID, householdID string, expiresAt time.Time) error {
	d.rows[string(tokenHash)] = &sessionRow{UserID: userID, HouseholdID: householdID, ExpiresAt: expiresAt}
	d.created++
	return nil
}

func (d *sessionDouble) ByTokenHash(_ context.Context, tokenHash []byte) (usecase.SessionRecord, error) {
	row, ok := d.rows[string(tokenHash)]
	if !ok || row.Revoked || !row.ExpiresAt.After(d.clock.Now()) {
		return usecase.SessionRecord{}, domain.ErrNotFound
	}
	return usecase.SessionRecord{UserID: row.UserID, HouseholdID: row.HouseholdID, ExpiresAt: row.ExpiresAt}, nil
}

func (d *sessionDouble) Extend(_ context.Context, tokenHash []byte, expiresAt time.Time) error {
	if row, ok := d.rows[string(tokenHash)]; ok {
		row.ExpiresAt = expiresAt
	}
	return nil // ExtendSession is :exec — an unknown token is a silent no-op.
}

func (d *sessionDouble) RevokeByToken(_ context.Context, tokenHash []byte) error {
	if row, ok := d.rows[string(tokenHash)]; ok {
		row.Revoked = true
	}
	return nil // RevokeSessionByToken is :exec — an unknown token is a silent no-op.
}

func (d *sessionDouble) RevokeAllForUser(_ context.Context, userID string) error {
	for _, row := range d.rows {
		if row.UserID == userID {
			row.Revoked = true
		}
	}
	return nil
}

// count reports rows ever created; live reports rows that are neither
// revoked nor expired against the fixture's clock.
func (d *sessionDouble) count() int { return d.created }

func (d *sessionDouble) live() int {
	n := 0
	for _, row := range d.rows {
		if !row.Revoked && row.ExpiresAt.After(d.clock.Now()) {
			n++
		}
	}
	return n
}

// --- LoginAttemptRepository -------------------------------------------

type attemptRecord struct {
	HouseholdID *string
	UserID      *string
	Email       string
	Succeeded   bool
	At          time.Time
}

type loginAttemptDouble struct {
	records []attemptRecord
}

func newLoginAttemptDouble() *loginAttemptDouble { return &loginAttemptDouble{} }

func (d *loginAttemptDouble) Record(_ context.Context, householdID, userID *string, email string, succeeded bool, at time.Time) error {
	d.records = append(d.records, attemptRecord{HouseholdID: householdID, UserID: userID, Email: email, Succeeded: succeeded, At: at})
	return nil
}

// FailuresSince and FailuresSinceForEmail mirror ListRecentFailures /
// ListRecentFailuresByEmail exactly, including the strict "at > since"
// comparison the SQL uses.
func (d *loginAttemptDouble) FailuresSince(_ context.Context, householdID string, since time.Time) ([]time.Time, error) {
	var out []time.Time
	for _, r := range d.records {
		if r.Succeeded || r.HouseholdID == nil || *r.HouseholdID != householdID {
			continue
		}
		if r.At.After(since) {
			out = append(out, r.At)
		}
	}
	return out, nil
}

func (d *loginAttemptDouble) FailuresSinceForEmail(_ context.Context, email string, since time.Time) ([]time.Time, error) {
	var out []time.Time
	for _, r := range d.records {
		if r.Succeeded || r.Email != email {
			continue
		}
		if r.At.After(since) {
			out = append(out, r.At)
		}
	}
	return out, nil
}

func (d *loginAttemptDouble) ClearFailures(_ context.Context, householdID string) error {
	kept := make([]attemptRecord, 0, len(d.records))
	for _, r := range d.records {
		if !r.Succeeded && r.HouseholdID != nil && *r.HouseholdID == householdID {
			continue
		}
		kept = append(kept, r)
	}
	d.records = kept
	return nil
}

// --- MagicLinkRepository ------------------------------------------------

type magicLinkRow struct {
	UserID     string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	ConsumedAt *time.Time
}

type magicLinkDouble struct {
	clock *fixedClock
	users *userDouble
	rows  map[string]*magicLinkRow // keyed by string(tokenHash)
}

func newMagicLinkDouble(clock *fixedClock, users *userDouble) *magicLinkDouble {
	return &magicLinkDouble{clock: clock, users: users, rows: map[string]*magicLinkRow{}}
}

func (d *magicLinkDouble) Create(_ context.Context, userID string, tokenHash []byte, expiresAt time.Time) error {
	d.rows[string(tokenHash)] = &magicLinkRow{UserID: userID, ExpiresAt: expiresAt, CreatedAt: d.clock.Now()}
	return nil
}

// Consume mirrors ConsumeMagicLink's guard — token_hash = $1 AND
// consumed_at IS NULL AND expires_at > now() — against the fixture's clock
// rather than wall time.
func (d *magicLinkDouble) Consume(_ context.Context, tokenHash []byte) (string, error) {
	row, ok := d.rows[string(tokenHash)]
	if !ok || row.ConsumedAt != nil || !row.ExpiresAt.After(d.clock.Now()) {
		return "", domain.ErrNotFound
	}
	now := d.clock.Now()
	row.ConsumedAt = &now
	return row.UserID, nil
}

func (d *magicLinkDouble) CountSince(_ context.Context, email string, since time.Time) (int, error) {
	n := 0
	for _, row := range d.rows {
		u, ok := d.users.byID[row.UserID]
		if !ok || u.Email != email {
			continue
		}
		if row.CreatedAt.After(since) {
			n++
		}
	}
	return n, nil
}

// --- Mailer ---------------------------------------------------------

type sentMail struct {
	To   string
	Name string
	URL  string
}

type mailerDouble struct {
	magicLinks []sentMail
	invites    []sentMail
}

func (d *mailerDouble) SendMagicLink(_ context.Context, to, name, url string) error {
	d.magicLinks = append(d.magicLinks, sentMail{To: to, Name: name, URL: url})
	return nil
}

func (d *mailerDouble) SendInvite(_ context.Context, to, name, inviterName, u string) error {
	d.invites = append(d.invites, sentMail{To: to, Name: name, URL: u})
	return nil
}

// lastMagicToken extracts the "token" query parameter from the most
// recently sent magic-link URL, failing the test if no magic link was sent.
func (d *mailerDouble) lastMagicToken(t *testing.T) string {
	t.Helper()
	if len(d.magicLinks) == 0 {
		t.Fatal("no magic link was sent")
	}
	last := d.magicLinks[len(d.magicLinks)-1]
	u, err := url.Parse(last.URL)
	if err != nil {
		t.Fatalf("magic link URL %q did not parse: %v", last.URL, err)
	}
	token := u.Query().Get("token")
	if token == "" {
		t.Fatalf("magic link URL %q carried no token parameter", last.URL)
	}
	return token
}

// --- fixture ----------------------------------------------------------

// fixture builds an AuthService over the in-memory doubles containing the
// design's household: Andreas with password "hunter2", Ethan with no
// password at all, both members of one household, and a fixedClock
// starting at 2026-07-18T09:41:00Z.
type fixture struct {
	auth        *usecase.AuthService
	clock       *fixedClock
	sessions    *sessionDouble
	mailer      *mailerDouble
	hasher      *fakeHasher
	householdID string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	clock := &fixedClock{now: time.Date(2026, 7, 18, 9, 41, 0, 0, time.UTC)}
	users := newUserDouble()
	members := newMembershipDouble(users)
	sessions := newSessionDouble(clock)
	attempts := newLoginAttemptDouble()
	magicLinks := newMagicLinkDouble(clock, users)
	mailer := &mailerDouble{}
	hasher := &fakeHasher{}

	householdID := "household-1"

	andreas := usecase.StoredUser{
		User:         domain.User{ID: "user-andreas", Email: "andreas@hearth.family", DisplayName: "Andreas"},
		PasswordHash: mustHash(t, hasher, "hunter2"),
	}
	users.put(andreas)
	members.put(domain.Membership{
		ID: "membership-andreas", HouseholdID: householdID, UserID: andreas.ID,
		Role: domain.RoleOwner, Capabilities: domain.AllCapabilities(),
	})

	ethan := usecase.StoredUser{
		User: domain.User{ID: "user-ethan", Email: "ethan@hearth.family", DisplayName: "Ethan"},
		// PasswordHash left empty: a credential-less member cannot sign in.
		// Ethan still has an email so TestUsersWithoutAPasswordCannotSignIn
		// exercises the "found but no password" branch, not the
		// unknown-address branch.
	}
	users.put(ethan)
	members.put(domain.Membership{
		ID: "membership-ethan", HouseholdID: householdID, UserID: ethan.ID,
		Role: domain.RoleLimited, Capabilities: domain.Capabilities{domain.CapChores},
	})

	auth := usecase.NewAuthService(usecase.AuthDeps{
		Users:      users,
		Members:    members,
		Sessions:   sessions,
		Attempts:   attempts,
		MagicLinks: magicLinks,
		Mailer:     mailer,
		Hasher:     hasher,
		Tokens:     &seqTokens{},
		Clock:      clock,
		SessionTTL: 30 * 24 * time.Hour,
		BaseURL:    "http://localhost:5173",
	})

	return &fixture{auth: auth, clock: clock, sessions: sessions, mailer: mailer, hasher: hasher, householdID: householdID}
}

func mustHash(t *testing.T, hasher usecase.PasswordHasher, plain string) string {
	t.Helper()
	hash, err := hasher.Hash(plain)
	if err != nil {
		t.Fatalf("hash %q: %v", plain, err)
	}
	return hash
}
