package usecase_test

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// readLog records the sequence of repository reads a service performed, in
// order. SignupService.Request's contract is that the same reads happen, in
// the same order, on every branch -- a read that is *skipped* on one branch
// is the exact defect RequestMagicLink shipped with (see auth.go's
// RequestMagicLink doc comment), and no assertion about return values can
// catch it. Only an ordered log can.
//
// It is wired into the synchronous read methods only (userDouble.ByEmail,
// signupDouble.CountSince, signupDouble.CountForEmailSince) -- never into
// mailerDouble, which is called from sendAsync's background goroutine. This
// type carries no mutex, and a log write from that goroutine racing a test's
// log.seq() read would be exactly the kind of bug -race is run to catch, not
// something this double should paper over.
type readLog struct{ calls []string }

func (l *readLog) record(name string) { l.calls = append(l.calls, name) }
func (l *readLog) seq() []string      { return l.calls }
func (l *readLog) reset()             { l.calls = nil }

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

type seqTokens struct {
	n int

	// failNextErr arms a one-shot failure for the next NewToken call, the
	// same one-shot pattern as the other doubles' failNext* hooks. It exists
	// so a signup test can exercise "token generation failed" -- entropy
	// exhaustion in the real generator -- without needing any change to
	// HashToken, which callers still use to look up the failed request's own
	// address hash.
	failNextErr error
}

func (t *seqTokens) NewToken() (string, []byte, error) {
	if t.failNextErr != nil {
		err := t.failNextErr
		t.failNextErr = nil
		return "", nil, err
	}
	t.n++
	raw := fmt.Sprintf("token-%d", t.n)
	return raw, t.HashToken(raw), nil
}
func (t *seqTokens) HashToken(raw string) []byte { return []byte("hash:" + raw) }

// failNext arms failNextErr: the next NewToken call returns err instead of a
// token, and every call after that succeeds normally again.
func (t *seqTokens) failNext(err error) { t.failNextErr = err }

// --- UserRepository ------------------------------------------------------

type userDouble struct {
	byID    map[string]usecase.StoredUser
	byEmail map[string]string // email -> id
	n       int

	// byEmailCalls counts ByEmail invocations. RequestMagicLink is meant to
	// call this exactly once per request regardless of outcome (see
	// magicLinkDouble.countSinceCalls for the matching count on the other
	// read) -- this field is how a test pins that down without resorting to
	// wall-clock timing. It is safe unguarded because every test that reads
	// it calls AuthService methods synchronously from the test goroutine
	// itself; only the mailer double's state is touched from a background
	// goroutine.
	byEmailCalls int

	// log, when set, additionally records "Users.ByEmail" into a shared
	// readLog -- SignupService.Request's ordered-read test uses this (see
	// readLog's doc comment). nil by default so every other fixture that
	// builds a userDouble is unaffected.
	log *readLog

	// members is set once, after construction (userDouble and
	// membershipDouble each need a reference to the other), and used only by
	// CreateWithMembership to mirror UserRepo.CreateWithMembership's
	// transaction: the user insert and the membership insert happen
	// together, and a failure in the second undoes the first.
	members *membershipDouble

	// failNextMembershipCreate arms a one-shot failure for the next
	// CreateWithMembership call's membership half, mirroring the real
	// owners_hold_all_capabilities constraint rejecting an invalid row
	// mid-transaction. It exists so a usecase-level test can prove the
	// double rolls back the user insert exactly as the real transaction
	// does, not just that the happy path works.
	failNextMembershipCreate error
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
	d.byEmailCalls++
	if d.log != nil {
		d.log.record("Users.ByEmail")
	}
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

// count reports how many users exist, for tests confirming an invite
// acceptance created exactly one — not zero, and not two from a botched
// retry.
func (d *userDouble) count() int { return len(d.byID) }

// setMembers completes the two doubles' mutual reference: newMembershipDouble
// already takes a *userDouble, and CreateWithMembership needs the reverse
// direction to create the membership half of its transaction.
func (d *userDouble) setMembers(m *membershipDouble) { d.members = m }

// failNextCreateWithMembership arms failNextMembershipCreate: the next
// CreateWithMembership call's membership insert returns err instead of
// succeeding, and the user insert that call already made is rolled back
// (removed from byID/byEmail) to mirror the real transaction's rollback.
// Every call after that succeeds normally again.
func (d *userDouble) failNextCreateWithMembership(err error) {
	d.failNextMembershipCreate = err
}

// CreateWithMembership mirrors UserRepo.CreateWithMembership: the user and
// membership are created together, and a failure in the membership half
// undoes the user insert rather than leaving it committed -- the same
// all-or-nothing guarantee the real transaction gives, reproduced here
// without an actual database.
func (d *userDouble) CreateWithMembership(ctx context.Context, email, passwordHash, displayName string,
	m domain.Membership) (domain.User, domain.Membership, error) {
	user, err := d.Create(ctx, email, passwordHash, displayName)
	if err != nil {
		return domain.User{}, domain.Membership{}, err
	}

	if d.failNextMembershipCreate != nil {
		err := d.failNextMembershipCreate
		d.failNextMembershipCreate = nil
		d.rollback(user)
		return domain.User{}, domain.Membership{}, err
	}

	m.UserID = user.ID
	membership, err := d.members.Create(ctx, m)
	if err != nil {
		d.rollback(user)
		return domain.User{}, domain.Membership{}, err
	}
	return user, membership, nil
}

// rollback undoes the user insert Create just made, for the two
// CreateWithMembership failure paths above -- the in-memory equivalent of
// the real transaction's Rollback.
func (d *userDouble) rollback(user domain.User) {
	delete(d.byID, user.ID)
	if user.Email != "" {
		delete(d.byEmail, user.Email)
	}
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

// FindOrphanedChild mirrors GetOrphanedCredentiallessUserByName: a
// credential-less user (no email, no password) with this display name that
// holds no membership row anywhere. It exists so seed.go's ensureChild can
// detect the state removing a membership without deleting its user leaves
// behind, instead of silently creating a duplicate under the same name.
func (d *userDouble) FindOrphanedChild(_ context.Context, displayName string) (domain.User, error) {
	for _, u := range d.byID {
		if u.DisplayName != displayName || u.Email != "" || u.PasswordHash != "" {
			continue
		}
		if _, hasMembership := d.members.byUser[u.ID]; hasMembership {
			continue
		}
		return u.User, nil
	}
	return domain.User{}, domain.ErrNotFound
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

// count reports how many memberships exist, the matching half of
// userDouble.count for invite-acceptance tests.
func (d *membershipDouble) count() int { return len(d.byID) }

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

	// failNextRevoke arms a one-shot failure for the next RevokeAllForUser
	// call, the same one-shot pattern userDouble.failNextCreateWithMembership
	// and magicLinkDouble.failNextCreate use. It exists so a usecase-level
	// test can prove MemberService reports (and logs) a revocation failure
	// distinctly from the membership mutation itself failing -- the real
	// SessionRepository.RevokeAllForUser can fail (a dead connection, a
	// statement timeout) even on an otherwise-successful Update or Remove,
	// and nothing exercised that path before this existed.
	failNextRevoke error
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

// failNextRevokeAllForUser arms failNextRevoke: the next RevokeAllForUser
// call returns err instead of revoking anything, and every call after that
// succeeds normally again.
func (d *sessionDouble) failNextRevokeAllForUser(err error) {
	d.failNextRevoke = err
}

func (d *sessionDouble) RevokeAllForUser(_ context.Context, userID string) error {
	if d.failNextRevoke != nil {
		err := d.failNextRevoke
		d.failNextRevoke = nil
		return err
	}
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

// liveForUser is live()'s per-user counterpart: it exists so a member-service
// test can prove RevokeAllForUser was scoped to the one member it touched --
// that member's live count drops to zero while an unrelated member's live
// session is left standing -- rather than merely proving "some session
// somewhere got revoked," which a bug that revoked every row in the table
// would also satisfy.
func (d *sessionDouble) liveForUser(userID string) int {
	n := 0
	for _, row := range d.rows {
		if row.UserID == userID && !row.Revoked && row.ExpiresAt.After(d.clock.Now()) {
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

// Prune mirrors PruneLoginAttempts: every row with At before the cutoff is
// deleted, including the NULL-HouseholdID rows ClearFailures cannot reach,
// and the count of deleted rows is returned.
func (d *loginAttemptDouble) Prune(_ context.Context, before time.Time) (int64, error) {
	kept := make([]attemptRecord, 0, len(d.records))
	var deleted int64
	for _, r := range d.records {
		if r.At.Before(before) {
			deleted++
			continue
		}
		kept = append(kept, r)
	}
	d.records = kept
	return deleted, nil
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

	// countSinceCalls counts CountSince invocations, the matching half of
	// userDouble.byEmailCalls for pinning "every outcome does the same
	// reads." Safe unguarded for the same reason byEmailCalls is: it is only
	// ever touched from the synchronous portion of RequestMagicLink, never
	// from the background send goroutine.
	countSinceCalls int

	// failNextCreate arms a one-shot failure for the next Create call, the
	// same one-shot pattern as mailerDouble.failNext. Unguarded for the same
	// reason as countSinceCalls: Create runs synchronously on the request
	// goroutine, never from sendMagicLinkAsync's goroutine, so nothing here
	// is ever touched concurrently.
	failNextCreate error
}

func newMagicLinkDouble(clock *fixedClock, users *userDouble) *magicLinkDouble {
	return &magicLinkDouble{clock: clock, users: users, rows: map[string]*magicLinkRow{}}
}

// count reports how many magic-link rows have ever been created, regardless
// of whether they were later consumed. It exists so a test can confirm a
// token was persisted even when the send that was supposed to follow it
// failed.
func (d *magicLinkDouble) count() int { return len(d.rows) }

// failNextMagicLinkCreate arms failNextCreate: the next call to Create
// returns err instead of persisting a row, and every call after that
// succeeds normally again.
func (d *magicLinkDouble) failNextMagicLinkCreate(err error) {
	d.failNextCreate = err
}

func (d *magicLinkDouble) Create(_ context.Context, userID string, tokenHash []byte, expiresAt time.Time) error {
	if d.failNextCreate != nil {
		err := d.failNextCreate
		d.failNextCreate = nil
		return err
	}
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
	d.countSinceCalls++
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

// --- InviteRepository -------------------------------------------------

type inviteRow struct {
	ID           string
	HouseholdID  string
	Email        string
	Name         string
	Role         domain.Role
	Capabilities domain.Capabilities
	InvitedBy    string
	ExpiresAt    time.Time
	AcceptedAt   *time.Time
}

// inviteDouble plays the same role invite_repo.go's InviteRepo plays over
// Postgres: ByTokenHash joins through users and a household name the same
// way GetInviteByTokenHash's SQL does, and Accept performs the user
// creation, membership creation and acceptance stamp together, mirroring
// the one-transaction guarantee the real Accept gives (see its doc comment
// in ports.go). It holds the same userDouble and membershipDouble the rest
// of the fixture uses, rather than private state of its own, so a test can
// check "exactly one user, exactly one membership" through those doubles
// after calling InviteService.Accept.
type inviteDouble struct {
	clock      *fixedClock
	users      *userDouble
	members    *membershipDouble
	familyName map[string]string // householdID -> family name, mirrors the households join
	rows       map[string]*inviteRow
	n          int

	// raceNextCreate arms a one-shot simulated race: the next Create call
	// writes its row (mirroring a concurrent writer's insert landing first)
	// and returns domain.ErrAlreadyExists instead of the row's ID, exactly
	// as translate maps the real UNIQUE (token_hash) constraint's violation.
	// It exists so a test can exercise the tolerance branch a check-then-
	// write caller (seed.go's issueChristineInviteAtNextRung) relies on for
	// the window between its own existence check and this call -- a branch
	// that plain concurrent double calls cannot otherwise reach
	// deterministically.
	raceNextCreate bool
}

func newInviteDouble(clock *fixedClock, users *userDouble, members *membershipDouble) *inviteDouble {
	return &inviteDouble{
		clock: clock, users: users, members: members,
		familyName: map[string]string{}, rows: map[string]*inviteRow{},
	}
}

func (d *inviteDouble) setFamilyName(householdID, name string) { d.familyName[householdID] = name }

// count reports how many invite rows have ever been created, for tests
// confirming the "limited member with no email" path writes no invite row
// at all, and the "rejected before any write" path writes none either.
func (d *inviteDouble) count() int { return len(d.rows) }

func (d *inviteDouble) byID(inviteID string) *inviteRow {
	for _, row := range d.rows {
		if row.ID == inviteID {
			return row
		}
	}
	return nil
}

// failNextCreateWithAlreadyExists arms raceNextCreate: the next Create call
// writes its row but reports domain.ErrAlreadyExists instead of its ID, and
// every call after that succeeds normally again.
func (d *inviteDouble) failNextCreateWithAlreadyExists() {
	d.raceNextCreate = true
}

func (d *inviteDouble) Create(_ context.Context, householdID, email, name string, role domain.Role,
	caps domain.Capabilities, tokenHash []byte, invitedBy string, expiresAt time.Time) (string, error) {
	if _, exists := d.rows[string(tokenHash)]; exists {
		// Mirrors the real UNIQUE (token_hash) constraint: a second Create
		// under a hash that already has a row is rejected outright, exactly
		// as translate maps Postgres's unique-violation error.
		return "", domain.ErrAlreadyExists
	}

	d.n++
	id := fmt.Sprintf("invite-%d", d.n)
	d.rows[string(tokenHash)] = &inviteRow{
		ID: id, HouseholdID: householdID, Email: email, Name: name, Role: role,
		Capabilities: caps, InvitedBy: invitedBy, ExpiresAt: expiresAt,
	}

	if d.raceNextCreate {
		d.raceNextCreate = false
		// The row above is exactly what a concurrent writer's insert would
		// have produced, landing between the caller's own existence check
		// and this call -- so the row must still exist afterwards, only the
		// return value differs from the ordinary success case.
		return "", domain.ErrAlreadyExists
	}
	return id, nil
}

func (d *inviteDouble) ByTokenHash(_ context.Context, tokenHash []byte) (usecase.InviteDetails, error) {
	row, ok := d.rows[string(tokenHash)]
	if !ok {
		return usecase.InviteDetails{}, domain.ErrNotFound
	}
	return d.details(row), nil
}

// LiveInviteForEmail mirrors GetLiveInviteForEmail: the invite, if any, for
// this address in this household that is neither accepted nor expired.
func (d *inviteDouble) LiveInviteForEmail(_ context.Context, householdID, email string) (usecase.InviteDetails, error) {
	now := d.clock.Now()
	for _, row := range d.rows {
		if row.HouseholdID != householdID || row.Email != email {
			continue
		}
		if row.AcceptedAt != nil || !row.ExpiresAt.After(now) {
			continue
		}
		return d.details(row), nil
	}
	return usecase.InviteDetails{}, domain.ErrNotFound
}

// details projects one row the same way ByTokenHash and LiveInviteForEmail
// both need to: joined to its inviter's display name and its household's
// family name, mirroring the real GetInviteByTokenHash / GetLiveInviteForEmail
// SQL joins.
func (d *inviteDouble) details(row *inviteRow) usecase.InviteDetails {
	inviter := d.users.byID[row.InvitedBy]
	return usecase.InviteDetails{
		ID: row.ID, HouseholdID: row.HouseholdID, Email: row.Email, Name: row.Name,
		Role: row.Role, Capabilities: row.Capabilities,
		FamilyName: d.familyName[row.HouseholdID], InviterName: inviter.DisplayName,
		ExpiresAt: row.ExpiresAt, AcceptedAt: row.AcceptedAt,
	}
}

// MarkAccepted mirrors MarkInviteAccepted's guarded update -- accepted_at IS
// NULL AND expires_at > now() -- reporting domain.ErrInviteAlreadyAccepted
// for either an already-accepted or an expired invite, exactly as the SQL's
// zero-rows result does.
func (d *inviteDouble) MarkAccepted(_ context.Context, inviteID string) error {
	row := d.byID(inviteID)
	if row == nil || row.AcceptedAt != nil || !row.ExpiresAt.After(d.clock.Now()) {
		return domain.ErrInviteAlreadyAccepted
	}
	now := d.clock.Now()
	row.AcceptedAt = &now
	return nil
}

// Accept performs the guarded acceptance stamp first, then the user and
// membership creation, in that order -- the same order invite_repo.go's
// Accept uses, and for the same reason: the guard is what makes a second,
// concurrent acceptance fail cheaply rather than colliding on the first
// acceptance's already-claimed email address.
func (d *inviteDouble) Accept(ctx context.Context, inviteID, email, passwordHash, displayName string,
	householdID string, role domain.Role, caps domain.Capabilities) (usecase.AcceptedInvite, error) {
	row := d.byID(inviteID)
	if row == nil || row.AcceptedAt != nil || !row.ExpiresAt.After(d.clock.Now()) {
		return usecase.AcceptedInvite{}, domain.ErrInviteAlreadyAccepted
	}
	now := d.clock.Now()
	row.AcceptedAt = &now

	user, err := d.users.Create(ctx, email, passwordHash, displayName)
	if err != nil {
		return usecase.AcceptedInvite{}, err
	}
	membership, err := domain.NewMembership("", householdID, user.ID, role, caps)
	if err != nil {
		return usecase.AcceptedInvite{}, err
	}
	created, err := d.members.Create(ctx, membership)
	if err != nil {
		return usecase.AcceptedInvite{}, err
	}
	return usecase.AcceptedInvite{UserID: user.ID, MembershipID: created.ID, HouseholdID: created.HouseholdID}, nil
}

// --- SignupRepository -------------------------------------------------

type signupRow struct {
	ID         string
	Email      string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	ConsumedAt *time.Time
}

// signupDouble plays the same role postgres's (future) SignupRepo plays over
// Postgres: Provision performs the household, owner user, owner membership,
// builtin-space and notification-preference writes together, mirroring the
// one-transaction guarantee the real Provision gives (see its doc comment in
// ports.go). It holds the same household/user/membership/space/notification
// doubles the rest of the fixture uses, rather than private state of its own,
// so a test can check "exactly one household, exactly one user, exactly one
// membership" through those doubles after calling SignupService.Complete --
// exactly the pattern inviteDouble already establishes for Accept.
type signupDouble struct {
	clock         *fixedClock
	households    *householdDouble
	users         *userDouble
	members       *membershipDouble
	spaces        *spaceDouble
	notifications *notificationDouble

	rows map[string]*signupRow // keyed by string(tokenHash)
	n    int

	// log, when set, additionally records "Signups.CountSince" and
	// "Signups.CountForEmailSince" into a shared readLog -- see readLog's
	// doc comment. nil by default so every other fixture that builds a
	// signupDouble is unaffected.
	log *readLog

	// emailCountOverride and globalCountOverride force CountForEmailSince and
	// CountSince to report a value regardless of how many rows actually
	// exist, so a test can put an address "at its hourly limit" or the
	// service "over its daily ceiling" without first creating however many
	// real rows that would take. A map entry's presence (not its value) is
	// what "overridden" means for emailCountOverride, so a forced count of 0
	// is still distinguishable from "no override set"; globalCountOverride
	// is a pointer for the identical reason.
	emailCountOverride  map[string]int
	globalCountOverride *int

	// provisions counts Provision invocations, successful or not, so a test
	// can confirm exactly one provision happened for one request -- the same
	// question userDouble.count/membershipDouble.count answer for invite
	// acceptance. Named provisions, not provisionCalls, so it does not
	// collide with the provisionCalls() method below -- Go forbids a field
	// and a method sharing a name on the same type.
	provisions int

	// failProvide arms a one-shot failure for the next Provision call, the
	// same one-shot pattern as the other doubles' failNext* hooks. Named
	// failProvide, not failNextProvision, for the same reason provisions is
	// not named provisionCalls: failNextProvision is the method a test calls
	// to arm it.
	failProvide error

	// failCreate arms a one-shot failure for the next Create call, letting a
	// signup test simulate "the signup insert fails" (a statement timeout, a
	// connection blip) the same way failProvide simulates a failed
	// Provision.
	failCreate error

	// lastPasswordHash records the passwordHash argument Provision was most
	// recently handed, so a test can confirm Complete hashed the caller's
	// password before ever reaching the repository rather than passing it
	// through raw.
	lastPasswordHash string
}

func newSignupDouble(clock *fixedClock, households *householdDouble, users *userDouble,
	members *membershipDouble, spaces *spaceDouble, notifications *notificationDouble) *signupDouble {
	return &signupDouble{
		clock: clock, households: households, users: users, members: members,
		spaces: spaces, notifications: notifications, rows: map[string]*signupRow{},
		emailCountOverride: map[string]int{},
	}
}

// failNextProvision arms failProvide: the next Provision call returns err
// instead of provisioning anything, and every call after that succeeds
// normally again.
func (d *signupDouble) failNextProvision(err error) { d.failProvide = err }

// failNextCreate arms failCreate: the next Create call returns err instead of
// persisting a row, and every call after that succeeds normally again.
func (d *signupDouble) failNextCreate(err error) { d.failCreate = err }

// setEmailCount forces the next CountForEmailSince call for this exact
// address to report n, regardless of how many rows for it actually exist.
func (d *signupDouble) setEmailCount(email string, n int) { d.emailCountOverride[email] = n }

// setGlobalCount forces the next CountSince call to report n, regardless of
// how many rows actually exist.
func (d *signupDouble) setGlobalCount(n int) { d.globalCountOverride = &n }

// markConsumed stamps the row for tokenHash consumed at at, for a test that
// needs a signup which has already been used -- Preview and Complete must
// both report ErrSignupAlreadyUsed for it, not ErrTokenExpired.
func (d *signupDouble) markConsumed(tokenHash []byte, at time.Time) {
	if row, ok := d.rows[string(tokenHash)]; ok {
		row.ConsumedAt = &at
	}
}

// createCount reports how many signup rows have ever been created.
func (d *signupDouble) createCount() int { return len(d.rows) }

// provisionCalls is a read of provisions, the matching accessor to the
// method-vs-field naming split provisions/failProvide use above.
func (d *signupDouble) provisionCalls() int { return d.provisions }

// lastProvisionPasswordHash is a read of lastPasswordHash.
func (d *signupDouble) lastProvisionPasswordHash() string { return d.lastPasswordHash }

func (d *signupDouble) byID(signupID string) *signupRow {
	for _, row := range d.rows {
		if row.ID == signupID {
			return row
		}
	}
	return nil
}

func (d *signupDouble) Create(_ context.Context, email string, tokenHash []byte, expiresAt time.Time) error {
	if d.failCreate != nil {
		err := d.failCreate
		d.failCreate = nil
		return err
	}
	d.n++
	d.rows[string(tokenHash)] = &signupRow{
		ID: fmt.Sprintf("signup-%d", d.n), Email: email,
		CreatedAt: d.clock.Now(), ExpiresAt: expiresAt,
	}
	return nil
}

// CreateConsumed mirrors CreateConsumedSignup: a row is written exactly like
// Create's, except ConsumedAt is stamped at insertion rather than left nil.
// It shares failCreate with Create -- both are "the signup insert failed",
// and a test arming one has no reason to care which of the two branches
// happens to call it.
func (d *signupDouble) CreateConsumed(_ context.Context, email string, tokenHash []byte, expiresAt time.Time) error {
	if d.failCreate != nil {
		err := d.failCreate
		d.failCreate = nil
		return err
	}
	d.n++
	now := d.clock.Now()
	d.rows[string(tokenHash)] = &signupRow{
		ID: fmt.Sprintf("signup-%d", d.n), Email: email,
		CreatedAt: now, ExpiresAt: expiresAt, ConsumedAt: &now,
	}
	return nil
}

func (d *signupDouble) ByTokenHash(_ context.Context, tokenHash []byte) (usecase.SignupDetails, error) {
	row, ok := d.rows[string(tokenHash)]
	if !ok {
		return usecase.SignupDetails{}, domain.ErrNotFound
	}
	return usecase.SignupDetails{
		ID: row.ID, Email: row.Email, ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt,
	}, nil
}

// CountForEmailSince mirrors CountSignupsForEmailSince: created_at >= since,
// matched by address, with no join through users -- there is no user to join
// to for a brand-new address. An override set via setEmailCount takes
// precedence over the real count, for a test that wants an address "at its
// hourly limit" without creating that many rows.
func (d *signupDouble) CountForEmailSince(_ context.Context, email string, since time.Time) (int, error) {
	if d.log != nil {
		d.log.record("Signups.CountForEmailSince")
	}
	if n, ok := d.emailCountOverride[email]; ok {
		return n, nil
	}
	n := 0
	for _, row := range d.rows {
		if row.Email == email && !row.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

// CountSince mirrors CountSignupsSince: created_at >= since, over every row
// regardless of address. An override set via setGlobalCount takes precedence
// over the real count, for the same reason CountForEmailSince's override
// does.
func (d *signupDouble) CountSince(_ context.Context, since time.Time) (int, error) {
	if d.log != nil {
		d.log.record("Signups.CountSince")
	}
	if d.globalCountOverride != nil {
		return *d.globalCountOverride, nil
	}
	n := 0
	for _, row := range d.rows {
		if !row.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

// Provision mirrors the real Provision's guarded consume-then-build: a
// signup that is already consumed or expired reports domain.ErrTokenExpired,
// collapsing the two cases into one answer exactly as
// InviteRepository.Accept's guarded UPDATE does, with nothing written in
// either case. Otherwise it builds the household from b, creates the owner
// user and membership, seeds the builtin spaces and sets the notification
// preferences, and stamps the signup consumed.
//
// Every write is undone on any later step's failure -- the same
// all-or-nothing guarantee userDouble.CreateWithMembership gives its two
// writes, extended here to five. This is not a nicety: Provision's whole
// reason to exist is that a partial provision leaves a users row occupying
// users.email's unique index with no membership under it, permanently
// blocking that address (see Provision's doc comment in ports.go). A double
// that left a partial write in place on a mid-sequence failure would hide
// exactly the defect this method is supposed to make impossible.
func (d *signupDouble) Provision(ctx context.Context, signupID, passwordHash string,
	b usecase.HouseholdBlueprint) (usecase.ProvisionedHousehold, error) {
	d.provisions++
	d.lastPasswordHash = passwordHash

	if d.failProvide != nil {
		err := d.failProvide
		d.failProvide = nil
		return usecase.ProvisionedHousehold{}, err
	}

	row := d.byID(signupID)
	if row == nil || domain.TokenLifecycle(d.clock.Now(), row.ExpiresAt, row.ConsumedAt) != domain.TokenLive {
		return usecase.ProvisionedHousehold{}, domain.ErrTokenExpired
	}

	household, err := d.households.Create(ctx, b.Household())
	if err != nil {
		return usecase.ProvisionedHousehold{}, err
	}

	user, err := d.users.Create(ctx, row.Email, passwordHash, b.OwnerDisplayName)
	if err != nil {
		delete(d.households.rows, household.ID)
		return usecase.ProvisionedHousehold{}, err
	}

	membership, err := domain.NewMembership("", household.ID, user.ID, b.OwnerRole, b.OwnerCapabilities)
	if err != nil {
		d.users.rollback(user)
		delete(d.households.rows, household.ID)
		return usecase.ProvisionedHousehold{}, err
	}
	created, err := d.members.Create(ctx, membership)
	if err != nil {
		d.users.rollback(user)
		delete(d.households.rows, household.ID)
		return usecase.ProvisionedHousehold{}, err
	}

	var madeSpaceIDs []string
	for _, s := range domain.BuiltinSpaces(household.ID) {
		created2, err := d.spaces.Create(ctx, s)
		if err != nil {
			d.removeSpaces(madeSpaceIDs)
			_ = d.members.Delete(ctx, household.ID, created.ID)
			d.users.rollback(user)
			delete(d.households.rows, household.ID)
			return usecase.ProvisionedHousehold{}, err
		}
		madeSpaceIDs = append(madeSpaceIDs, created2.ID)
	}

	if _, err := d.notifications.Upsert(ctx, household.ID, b.Notifications); err != nil {
		d.removeSpaces(madeSpaceIDs)
		_ = d.members.Delete(ctx, household.ID, created.ID)
		d.users.rollback(user)
		delete(d.households.rows, household.ID)
		return usecase.ProvisionedHousehold{}, err
	}

	now := d.clock.Now()
	row.ConsumedAt = &now

	return usecase.ProvisionedHousehold{
		UserID: user.ID, HouseholdID: household.ID, MembershipID: created.ID,
	}, nil
}

// removeSpaces undoes a partial run of Provision's builtin-space loop, for
// its own rollback paths -- spaceDouble has no Delete of its own (nothing
// else in this codebase ever removes a space), so this reaches into its
// rows directly rather than adding a method no real caller needs.
func (d *signupDouble) removeSpaces(ids []string) {
	if len(ids) == 0 {
		return
	}
	drop := make(map[string]bool, len(ids))
	for _, id := range ids {
		drop[id] = true
	}
	kept := d.spaces.rows[:0]
	for _, s := range d.spaces.rows {
		if !drop[s.ID] {
			kept = append(kept, s)
		}
	}
	d.spaces.rows = kept
}

// Prune mirrors PruneSignups: created_at < before AND (consumed or expired).
// A live, unexpired row is never pruned no matter how old before is.
func (d *signupDouble) Prune(_ context.Context, before time.Time) (int64, error) {
	var deleted int64
	for hash, row := range d.rows {
		expiredOrConsumed := row.ConsumedAt != nil || !row.ExpiresAt.After(d.clock.Now())
		if row.CreatedAt.Before(before) && expiredOrConsumed {
			delete(d.rows, hash)
			deleted++
		}
	}
	return deleted, nil
}

var _ usecase.SignupRepository = (*signupDouble)(nil)

// --- Mailer ---------------------------------------------------------

type sentMail struct {
	To   string
	Name string
	URL  string
}

// signupMail is sentMail without a Name: neither SendSignupLink nor
// SendSignupForExistingAccount carries one (see Mailer's doc comments in
// ports.go for why).
type signupMail struct {
	To  string
	URL string
}

// mailerDouble is touched from two goroutines now that
// AuthService.RequestMagicLink sends off the request path: the test's own
// goroutine, and the background goroutine the service spawns to call
// SendMagicLink. mu guards every field below for that reason. sent is
// signalled once per SendMagicLink call, success or failure alike, purely
// as a synchronization point -- "the double has been called and its state
// is now settled" -- so a test can wait for the async send to land instead
// of racing it. Waiting on a channel rather than sleeping is what keeps
// these tests race-detector-clean and non-flaky.
type mailerDouble struct {
	mu                     sync.Mutex
	magicLinks             []sentMail
	invites                []sentMail
	signupLinks            []signupMail
	existingAccountNotices []signupMail
	failNext               error
	panicNext              string

	// sendErr, unlike failNext, is not one-shot: it stays armed until cleared,
	// mirroring a relay that is down for the rest of the test rather than one
	// bad send. It gates only the two sign-up sends -- SendSignupLink and
	// SendSignupForExistingAccount -- because failNext already covers
	// SendMagicLink's one-shot failure case and nothing here needs to change
	// that behaviour.
	sendErr error

	sent chan struct{}
}

func newMailerDouble() *mailerDouble {
	return &mailerDouble{sent: make(chan struct{}, 64)}
}

// failNextMagicLink arms a one-shot failure: the next SendMagicLink call
// returns err instead of recording a sent mail, and every call after that
// succeeds normally again.
func (d *mailerDouble) failNextMagicLink(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failNext = err
}

// panicNextMagicLink arms a one-shot panic: the next SendMagicLink call
// panics with msg instead of returning, and every call after that behaves
// normally again. It exists to prove sendMagicLinkAsync's recover() (see
// auth.go) actually stops a panic in the send from escaping its goroutine —
// without it, this panic would crash the whole test binary, not just fail
// an assertion.
func (d *mailerDouble) panicNextMagicLink(msg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.panicNext = msg
}

func (d *mailerDouble) SendMagicLink(_ context.Context, to, name, url string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer d.signalSent()
	if d.panicNext != "" {
		msg := d.panicNext
		d.panicNext = ""
		panic(msg)
	}
	if d.failNext != nil {
		err := d.failNext
		d.failNext = nil
		return err
	}
	d.magicLinks = append(d.magicLinks, sentMail{To: to, Name: name, URL: url})
	return nil
}

func (d *mailerDouble) SendInvite(_ context.Context, to, name, inviterName, u string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.invites = append(d.invites, sentMail{To: to, Name: name, URL: u})
	return nil
}

// invitesSentCount is a mutex-guarded read of len(invites). Unlike
// SendMagicLink, InviteService.Create calls SendInvite synchronously on the
// caller's goroutine, so there is no background send to wait for here —
// this can be read immediately after Create returns.
func (d *mailerDouble) invitesSentCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.invites)
}

// lastInviteURL is a mutex-guarded read of the most recently sent invite
// email's URL, for a test that just called Create and wants the token that
// was mailed out.
func (d *mailerDouble) lastInviteURL() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.invites[len(d.invites)-1].URL
}

// SendSignupLink and SendSignupForExistingAccount record into their own
// slices rather than sharing one -- so a test can assert *which* of the two
// sign-up emails went out, the same distinction that oracle
// (SendSignupForExistingAccount's doc comment in ports.go) depends on a test
// being able to make. Both are signalled through the same sent channel every
// other Send* method uses, and both honour sendErr, since
// SignupService.sendAsync fires them off the request path exactly as
// sendMagicLinkAsync does.
func (d *mailerDouble) SendSignupLink(_ context.Context, to, url string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer d.signalSent()
	if d.sendErr != nil {
		return d.sendErr
	}
	d.signupLinks = append(d.signupLinks, signupMail{To: to, URL: url})
	return nil
}

func (d *mailerDouble) SendSignupForExistingAccount(_ context.Context, to, signInURL string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	defer d.signalSent()
	if d.sendErr != nil {
		return d.sendErr
	}
	d.existingAccountNotices = append(d.existingAccountNotices, signupMail{To: to, URL: signInURL})
	return nil
}

// failEverySend arms sendErr: every subsequent SendSignupLink and
// SendSignupForExistingAccount call returns err instead of recording a sent
// mail, until a test clears it (no test currently does; the scenario it
// models -- a relay that stays down -- has no reason to recover mid-test).
func (d *mailerDouble) failEverySend(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sendErr = err
}

// signupLinksSentCount is a mutex-guarded read of len(signupLinks).
func (d *mailerDouble) signupLinksSentCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.signupLinks)
}

// lastSignupLinkURL is a mutex-guarded read of the most recently sent
// sign-up link email's URL.
func (d *mailerDouble) lastSignupLinkURL() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.signupLinks[len(d.signupLinks)-1].URL
}

// existingAccountNoticesSentCount is a mutex-guarded read of
// len(existingAccountNotices).
func (d *mailerDouble) existingAccountNoticesSentCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.existingAccountNotices)
}

// signalSent must be called with mu held. The channel is large enough that
// no realistic test sequence fills it; a full channel drops the signal
// rather than blocking the mailer (and thus the service's background
// goroutine) forever.
//
// Invariant callers of waitForSend must respect: drain exactly one signal
// per send you expect to have happened, in order, before reading
// magicLinks/sentCount. The channel is a bare counter with no identity —
// draining fewer signals than were sent leaves extras buffered (harmless,
// the next waitForSend just returns immediately), but draining fewer than
// expected before *reading state* means you may be reading a snapshot from
// an earlier send than the one you meant to wait for. Every test in this
// package drains exactly once per expected send for exactly this reason.
func (d *mailerDouble) signalSent() {
	select {
	case d.sent <- struct{}{}:
	default:
	}
}

// waitForSend blocks until RequestMagicLink's background goroutine has
// called SendMagicLink at least once since the last time this was drained,
// or fails the test after a generous timeout. Tests need this because the
// send is fire-and-forget from the caller's point of view: by the time
// RequestMagicLink returns, the goroutine may not have run yet, so reading
// magicLinks immediately afterward would be a data race as well as flaky.
func (d *mailerDouble) waitForSend(t *testing.T) {
	t.Helper()
	select {
	case <-d.sent:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the async magic-link send")
	}
}

// waitForSends is waitForSend's counted form: it drains n signals from sent,
// each with the same generous timeout, for a signup test that needs to know
// the background send has landed before reading signupLinks or
// existingAccountNotices. Like waitForSend, it exists because the send is
// fire-and-forget from SignupService.Request's point of view -- by the time
// Request returns, sendAsync's goroutine may not have run yet.
func (d *mailerDouble) waitForSends(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-d.sent:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for async send %d of %d", i+1, n)
		}
	}
}

// assertNoSendsWithin is the one place a signup test waits on the clock
// rather than a synchronization channel, because proving a *negative* about
// an asynchronous send -- "nothing was sent" -- has no alternative: there is
// no event to wait for when the correct behaviour is that no event occurs.
// It fails the test if any send signal arrives before d elapses.
func (d *mailerDouble) assertNoSendsWithin(t *testing.T, wait time.Duration) {
	t.Helper()
	select {
	case <-d.sent:
		t.Fatal("a send happened when none was expected")
	case <-time.After(wait):
	}
}

// lastMagicToken waits for the most recent RequestMagicLink call's
// background send to land, then extracts the "token" query parameter from
// the most recently sent magic-link URL, failing the test if no magic link
// was ever sent.
func (d *mailerDouble) lastMagicToken(t *testing.T) string {
	t.Helper()
	d.waitForSend(t)

	d.mu.Lock()
	defer d.mu.Unlock()
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

// sentCount is a mutex-guarded read of len(magicLinks), for tests that
// already called waitForSend (or know no send is pending) and just want the
// count without racing the mailer's internal state.
func (d *mailerDouble) sentCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.magicLinks)
}

// magicLinkURLAt is a mutex-guarded read of magicLinks[i].URL, for the same
// reason as sentCount.
func (d *mailerDouble) magicLinkURLAt(i int) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.magicLinks[i].URL
}

// --- HouseholdRepository -----------------------------------------------

type householdDouble struct {
	rows map[string]domain.Household
	n    int
}

func newHouseholdDouble() *householdDouble {
	return &householdDouble{rows: map[string]domain.Household{}}
}

func (d *householdDouble) put(h domain.Household) { d.rows[h.ID] = h }

func (d *householdDouble) Get(_ context.Context, householdID string) (domain.Household, error) {
	h, ok := d.rows[householdID]
	if !ok {
		return domain.Household{}, domain.ErrNotFound
	}
	return h, nil
}

// Update mirrors HouseholdRepo.Update: it persists every field on h, not a
// narrowed subset -- see that repo's doc comment in
// internal/adapter/postgres/household_repo.go for the defect this guards
// against (a query that silently dropped Name and SecondaryCurrency while
// still returning a nil error).
func (d *householdDouble) Update(_ context.Context, h domain.Household) (domain.Household, error) {
	if _, ok := d.rows[h.ID]; !ok {
		return domain.Household{}, domain.ErrNotFound
	}
	d.rows[h.ID] = h
	return h, nil
}

// Create mirrors HouseholdRepo.Create: it persists every field on h except ID
// (assigned here, the way the database assigns it) and FXRateMode (always
// "auto", the column default -- see that repo's doc comment in
// internal/adapter/postgres/household_repo.go for why nothing else is safe to
// assume at creation time).
func (d *householdDouble) Create(_ context.Context, h domain.Household) (domain.Household, error) {
	d.n++
	h.ID = fmt.Sprintf("household-%d", d.n)
	h.FXRateMode = "auto"
	d.rows[h.ID] = h
	return h, nil
}

// --- SpaceRepository -----------------------------------------------------

// spaceDouble sorts List's result by Position, mirroring ListSpaces' own
// ORDER BY position (internal/adapter/postgres/queries/identity.sql) --
// domain.VisibleSpaces relies on its input already being in that order and
// does not sort itself, so a double that returned insertion order instead
// would let a test pass even if a caller forgot the sort was the repo's job,
// not the domain's.
type spaceDouble struct {
	rows []domain.Space
	n    int

	// failNextCreate arms a one-shot failure for the next Create call, the
	// same one-shot pattern magicLinkDouble.failNextCreate uses. It exists so
	// a test can simulate the race HouseholdService.CreateSpace's
	// list-then-compare pre-check cannot close on its own: a concurrent
	// creator wins on the same derived key, the real Postgres adapter
	// reports that as domain.ErrAlreadyExists (translate's pgconn.PgError/
	// 23505 case), and this double reproduces exactly that error without
	// needing two real concurrent callers.
	failNextCreate error
}

func newSpaceDouble() *spaceDouble { return &spaceDouble{} }

// seed adds spaces with IDs already assigned, for builtins constructed via
// domain.BuiltinSpaces (which deliberately leaves ID empty -- see that
// function's doc comment) that a fixture wants to look pre-seeded, as if the
// database had already assigned them.
func (d *spaceDouble) seed(spaces ...domain.Space) {
	for _, s := range spaces {
		d.n++
		if s.ID == "" {
			s.ID = fmt.Sprintf("space-%d", d.n)
		}
		d.rows = append(d.rows, s)
	}
}

func (d *spaceDouble) List(_ context.Context, householdID string) ([]domain.Space, error) {
	var out []domain.Space
	for _, s := range d.rows {
		if s.HouseholdID == householdID {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

// failNextSpaceCreate arms failNextCreate: the next Create call returns err
// instead of persisting a row, and every call after that succeeds normally
// again.
func (d *spaceDouble) failNextSpaceCreate(err error) {
	d.failNextCreate = err
}

func (d *spaceDouble) Create(_ context.Context, s domain.Space) (domain.Space, error) {
	if d.failNextCreate != nil {
		err := d.failNextCreate
		d.failNextCreate = nil
		return domain.Space{}, err
	}
	d.n++
	s.ID = fmt.Sprintf("space-%d", d.n)
	d.rows = append(d.rows, s)
	return s, nil
}

func (d *spaceDouble) NextPosition(_ context.Context, householdID string) (int, error) {
	max := 0
	for _, s := range d.rows {
		if s.HouseholdID == householdID && s.Position > max {
			max = s.Position
		}
	}
	return max + 1, nil
}

// --- NotificationRepository ----------------------------------------------

type notificationDouble struct {
	rows map[string]usecase.NotificationPreferences
}

func newNotificationDouble() *notificationDouble {
	return &notificationDouble{rows: map[string]usecase.NotificationPreferences{}}
}

func (d *notificationDouble) put(householdID string, p usecase.NotificationPreferences) {
	d.rows[householdID] = p
}

func (d *notificationDouble) Get(_ context.Context, householdID string) (usecase.NotificationPreferences, error) {
	p, ok := d.rows[householdID]
	if !ok {
		return usecase.NotificationPreferences{}, domain.ErrNotFound
	}
	return p, nil
}

// Upsert mirrors UpsertNotificationPreferences' ON CONFLICT ... DO UPDATE:
// a household with no row yet gets one created, an existing row is replaced
// wholesale.
func (d *notificationDouble) Upsert(_ context.Context, householdID string, p usecase.NotificationPreferences) (usecase.NotificationPreferences, error) {
	d.rows[householdID] = p
	return p, nil
}

// --- fixture ----------------------------------------------------------

// fixture builds an AuthService over the in-memory doubles containing the
// design's household: Andreas with password "hunter2", Ethan with no
// password at all, both members of one household, and a fixedClock
// starting at 2026-07-18T09:41:00Z.
type fixture struct {
	auth          *usecase.AuthService
	invites       *usecase.InviteService
	memberSvc     *usecase.MemberService
	householdSvc  *usecase.HouseholdService
	clock         *fixedClock
	sessions      *sessionDouble
	mailer        *mailerDouble
	hasher        *fakeHasher
	users         *userDouble
	members       *membershipDouble
	magicLinks    *magicLinkDouble
	inviteRepo    *inviteDouble
	households    *householdDouble
	spaces        *spaceDouble
	notifications *notificationDouble
	householdID   string
	andreasID     string
	ethanID       string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	clock := &fixedClock{now: time.Date(2026, 7, 18, 9, 41, 0, 0, time.UTC)}
	users := newUserDouble()
	members := newMembershipDouble(users)
	users.setMembers(members)
	sessions := newSessionDouble(clock)
	attempts := newLoginAttemptDouble()
	magicLinks := newMagicLinkDouble(clock, users)
	mailer := newMailerDouble()
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

	inviteRepo := newInviteDouble(clock, users, members)
	// Matches the design's household throughout: households.Create("Andreas
	// & Christine", "Oentoro") in invite_repo_test.go, and Andreas as the
	// inviter whose display name every invite-preview test expects.
	inviteRepo.setFamilyName(householdID, "Oentoro")

	invites := usecase.NewInviteService(usecase.InviteDeps{
		Invites:    inviteRepo,
		Users:      users,
		Sessions:   sessions,
		Mailer:     mailer,
		Hasher:     hasher,
		Tokens:     &seqTokens{},
		Clock:      clock,
		SessionTTL: 30 * 24 * time.Hour,
		BaseURL:    "http://localhost:5173",
	})

	memberSvc := usecase.NewMemberService(usecase.MemberDeps{
		Members:  members,
		Sessions: sessions,
	})

	households := newHouseholdDouble()
	households.put(domain.Household{
		ID: householdID, Name: "Andreas & Christine", FamilyName: "Oentoro",
		PrimaryCurrency: "SGD", ShowSecondaryCurrency: true, SecondaryCurrency: "IDR", FXRateMode: "auto",
	})

	spaces := newSpaceDouble()
	spaces.seed(domain.BuiltinSpaces(householdID)...)

	notifications := newNotificationDouble()
	notifications.put(householdID, usecase.NotificationPreferences{
		BillReminders: true, OverspendAlerts: true, RetroReminder: true, WeeklyDigest: true,
	})

	householdSvc := usecase.NewHouseholdService(usecase.HouseholdDeps{
		Households:    households,
		Spaces:        spaces,
		Notifications: notifications,
	})

	return &fixture{
		auth: auth, invites: invites, memberSvc: memberSvc, householdSvc: householdSvc,
		clock: clock, sessions: sessions, mailer: mailer, hasher: hasher,
		users: users, members: members, magicLinks: magicLinks, inviteRepo: inviteRepo,
		households: households, spaces: spaces, notifications: notifications,
		householdID: householdID, andreasID: andreas.ID, ethanID: ethan.ID,
	}
}

func mustHash(t *testing.T, hasher usecase.PasswordHasher, plain string) string {
	t.Helper()
	hash, err := hasher.Hash(plain)
	if err != nil {
		t.Fatalf("hash %q: %v", plain, err)
	}
	return hash
}

// --- AccountRepository ---------------------------------------------------

// fakeAccountRepo is the in-memory AccountRepository every AccountService test
// runs against. memberships maps a membership id to the household it belongs
// to, which is all MembershipBelongsToHousehold needs to answer.
type fakeAccountRepo struct {
	accounts    map[string]domain.Account
	memberships map[string]string
	nextID      int
}

func newFakeAccountRepo() *fakeAccountRepo {
	return &fakeAccountRepo{
		accounts:    map[string]domain.Account{},
		memberships: map[string]string{},
	}
}

func (r *fakeAccountRepo) List(_ context.Context, householdID string, includeArchived bool) ([]usecase.AccountView, error) {
	var out []usecase.AccountView
	for _, a := range r.accounts {
		if a.HouseholdID != householdID {
			continue
		}
		if a.IsArchived() && !includeArchived {
			continue
		}
		out = append(out, usecase.AccountView{
			Account: a,
			Balance: a.OpeningBalance,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Account.ID < out[j].Account.ID })
	return out, nil
}

func (r *fakeAccountRepo) Get(_ context.Context, householdID, accountID string) (usecase.AccountView, error) {
	a, ok := r.accounts[accountID]
	if !ok || a.HouseholdID != householdID {
		return usecase.AccountView{}, domain.ErrNotFound
	}
	return usecase.AccountView{Account: a, Balance: a.OpeningBalance}, nil
}

func (r *fakeAccountRepo) Create(_ context.Context, a domain.Account) (domain.Account, error) {
	r.nextID++
	a.ID = fmt.Sprintf("acct-%d", r.nextID)
	a.ArchivedAt = nil
	r.accounts[a.ID] = a
	return a, nil
}

func (r *fakeAccountRepo) Update(_ context.Context, a domain.Account) (domain.Account, error) {
	existing, ok := r.accounts[a.ID]
	if !ok || existing.HouseholdID != a.HouseholdID {
		return domain.Account{}, domain.ErrNotFound
	}
	a.ArchivedAt = existing.ArchivedAt
	r.accounts[a.ID] = a
	return a, nil
}

func (r *fakeAccountRepo) SetArchived(_ context.Context, householdID, accountID string, archived bool, at time.Time) (domain.Account, error) {
	a, ok := r.accounts[accountID]
	if !ok || a.HouseholdID != householdID {
		return domain.Account{}, domain.ErrNotFound
	}
	if archived {
		stamp := at
		a.ArchivedAt = &stamp
	} else {
		a.ArchivedAt = nil
	}
	r.accounts[accountID] = a
	return a, nil
}

func (r *fakeAccountRepo) MembershipBelongsToHousehold(_ context.Context, householdID, membershipID string) (bool, error) {
	return r.memberships[membershipID] == householdID, nil
}

// fakeCategoryRepo records how many times EnsureSeeded actually inserted, so a
// test can tell "seeded once" from "seeded on every call".
type fakeCategoryRepo struct {
	categories []domain.Category
	seeds      int
	nextID     int
}

func (f *fakeCategoryRepo) List(_ context.Context, householdID string, includeArchived bool) ([]domain.Category, error) {
	out := []domain.Category{}
	for _, c := range f.categories {
		if c.HouseholdID != householdID {
			continue
		}
		if c.IsArchived() && !includeArchived {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

func (f *fakeCategoryRepo) EnsureSeeded(_ context.Context, householdID string, starter []domain.Category) error {
	for _, c := range f.categories {
		if c.HouseholdID == householdID {
			return nil
		}
	}
	f.seeds++
	for _, c := range starter {
		f.nextID++
		c.ID = fmt.Sprintf("cat-%d", f.nextID)
		c.HouseholdID = householdID
		f.categories = append(f.categories, c)
	}
	return nil
}

// nameTaken reports whether name is already used by another category in this
// household, archived rows included -- the same collision the database's
// UNIQUE (household_id, name) enforces regardless of archived_at. excludeID
// lets Rename check against every row but its own.
func (f *fakeCategoryRepo) nameTaken(householdID, name, excludeID string) bool {
	for _, c := range f.categories {
		if c.HouseholdID == householdID && c.Name == name && c.ID != excludeID {
			return true
		}
	}
	return false
}

func (f *fakeCategoryRepo) Create(_ context.Context, c domain.Category) (domain.Category, error) {
	if f.nameTaken(c.HouseholdID, c.Name, "") {
		return domain.Category{}, domain.ErrCategoryNameTaken
	}
	f.nextID++
	c.ID = fmt.Sprintf("cat-%d", f.nextID)
	c.ArchivedAt = nil
	maxOrder := 0
	for _, existing := range f.categories {
		if existing.HouseholdID == c.HouseholdID && existing.SortOrder > maxOrder {
			maxOrder = existing.SortOrder
		}
	}
	c.SortOrder = maxOrder + 1
	f.categories = append(f.categories, c)
	return c, nil
}

func (f *fakeCategoryRepo) Rename(_ context.Context, householdID, categoryID, name string) (domain.Category, error) {
	for i, c := range f.categories {
		if c.ID != categoryID {
			continue
		}
		if c.HouseholdID != householdID {
			return domain.Category{}, domain.ErrNotFound
		}
		if f.nameTaken(householdID, name, categoryID) {
			return domain.Category{}, domain.ErrCategoryNameTaken
		}
		c.Name = name
		f.categories[i] = c
		return c, nil
	}
	return domain.Category{}, domain.ErrNotFound
}

// SetArchived only stamps ArchivedAt the first time a category is archived,
// so calling it again with archived=true is a true no-op rather than moving
// the timestamp forward -- the idempotence the port's doc comment promises.
func (f *fakeCategoryRepo) SetArchived(_ context.Context, householdID, categoryID string, archived bool) (domain.Category, error) {
	for i, c := range f.categories {
		if c.ID != categoryID {
			continue
		}
		if c.HouseholdID != householdID {
			return domain.Category{}, domain.ErrNotFound
		}
		if archived {
			if c.ArchivedAt == nil {
				now := time.Now().UTC()
				c.ArchivedAt = &now
			}
		} else {
			c.ArchivedAt = nil
		}
		f.categories[i] = c
		return c, nil
	}
	return domain.Category{}, domain.ErrNotFound
}

// budgetKey collapses a household and month into the map key fakeBudgetRepo
// uses. Budget.Month is documented as "any instant in the month", so the key
// normalizes to the first of the month the same way the database's UNIQUE
// (household_id, month) constraint would, rather than trusting every caller
// to have already truncated it.
func budgetKey(householdID string, month time.Time) string {
	return householdID + "|" + time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC).Format("2006-01")
}

// fakeBudgetRepo is the in-memory BudgetRepository every BudgetService test
// runs against, one row per household-month -- the map-backed shape the
// port's Get/Upsert/History contract calls for.
type fakeBudgetRepo struct {
	budgets map[string]domain.Budget
	nextID  int

	// knownCategoryIDs, when non-nil, is the set Upsert checks every line's
	// CategoryID against before writing anything -- mirroring the real
	// repository's own validateLineCategories (budget_repo.go), which
	// refuses a line whose category is not this household's inside the same
	// transaction as the write. It exists so a usecase-level test can prove
	// BudgetService.Save lets that error pass through untouched rather than
	// pre-validating category ownership itself (a check that belongs to the
	// repository, which is the thing that actually knows what a household
	// owns). nil means "accept anything", the default every other test gets.
	knownCategoryIDs map[string]bool

	// rolledOver holds, per household-month key (the same key r.budgets
	// uses), the goal id a month's unspent money was rolled into. Task 9
	// widened domain.Budget itself to carry RolledOverAt/RolloverGoalID, so
	// this map is no longer the only place that state lives -- RollOverToGoal
	// and clearRolloverStamp below keep both in sync -- but it stays as the
	// quick "is this month currently rolled over" read
	// rolledOverGoalID gives tests, rather than every caller re-deriving that
	// from a stored domain.Budget's own fields.
	rolledOver map[string]string

	// goals is the GoalRepository double RollOverToGoal writes its
	// contribution into, and that DeleteContribution reaches back through to
	// clear rolledOver -- the same mutual-reference pattern userDouble and
	// membershipDouble use for CreateWithMembership. nil until setGoals is
	// called; a fixture that never exercises rollover (every fixture as of
	// Task 3, since nothing consumes RollOverToGoal yet) has no reason to
	// wire it up.
	goals *goalDouble
}

func newFakeBudgetRepo() *fakeBudgetRepo {
	return &fakeBudgetRepo{budgets: map[string]domain.Budget{}, rolledOver: map[string]string{}}
}

// setGoals completes the mutual reference RollOverToGoal and
// goalDouble.DeleteContribution both need: this double writes into goals on
// a rollover, and goalDouble reaches back through its own budgets field to
// clear rolledOver when a rollover contribution is deleted. Call it once,
// after both doubles are constructed, exactly as userDouble.setMembers is
// called for its pair.
func (r *fakeBudgetRepo) setGoals(g *goalDouble) { r.goals = g }

// rolledOverGoalID is a read of rolledOver, for a test proving
// RollOverToGoal stamped a month or DeleteContribution cleared it back off
// again -- the double's only way to answer "is this month currently
// rolled over" without a domain.Budget field to hold the question.
func (r *fakeBudgetRepo) rolledOverGoalID(householdID string, month time.Time) (string, bool) {
	goalID, ok := r.rolledOver[budgetKey(householdID, month)]
	return goalID, ok
}

// clearRolloverStamp removes household+month's rollover stamp, if any. It is
// unexported and called only from goalDouble.DeleteContribution, mirroring
// RollOverToGoal's own port doc comment: deleting a budget_rollover
// contribution must clear the month's stamp in the same operation, or the
// household is left claiming a rollover that no longer has a contribution
// behind it.
func (r *fakeBudgetRepo) clearRolloverStamp(householdID string, month time.Time) {
	key := budgetKey(householdID, month)
	delete(r.rolledOver, key)
	if b, ok := r.budgets[key]; ok {
		b.RolledOverAt = nil
		b.RolloverGoalID = ""
		b.RolloverAmountMinor = nil
		r.budgets[key] = b
	}
}

// knownCategories arms the unknown-category check Upsert enforces below.
func (r *fakeBudgetRepo) knownCategories(ids ...string) {
	r.knownCategoryIDs = make(map[string]bool, len(ids))
	for _, id := range ids {
		r.knownCategoryIDs[id] = true
	}
}

func (r *fakeBudgetRepo) Get(_ context.Context, householdID string, month time.Time) (domain.Budget, error) {
	b, ok := r.budgets[budgetKey(householdID, month)]
	if !ok {
		return domain.Budget{}, domain.ErrNotFound
	}
	return b, nil
}

// Upsert always replaces every line wholesale, never merges -- the same
// full-replace contract Upsert's port doc comment requires of the real
// repository. b.ID and any line IDs the caller passed are ignored; an
// existing row keeps its own ID, a new one is assigned here.
func (r *fakeBudgetRepo) Upsert(_ context.Context, b domain.Budget) (domain.Budget, error) {
	if r.knownCategoryIDs != nil {
		for _, line := range b.Lines {
			if !r.knownCategoryIDs[line.CategoryID] {
				return domain.Budget{}, fmt.Errorf("fakeBudgetRepo: category %q does not belong to household %s",
					line.CategoryID, b.HouseholdID)
			}
		}
	}
	month := time.Date(b.Month.Year(), b.Month.Month(), 1, 0, 0, 0, 0, time.UTC)
	key := budgetKey(b.HouseholdID, month)
	if existing, ok := r.budgets[key]; ok {
		b.ID = existing.ID
	} else {
		r.nextID++
		b.ID = fmt.Sprintf("budget-%d", r.nextID)
	}
	b.Month = month
	lines := make([]domain.BudgetLine, len(b.Lines))
	copy(lines, b.Lines)
	b.Lines = lines
	r.budgets[key] = b
	return b, nil
}

// History walks backward from the viewed month, including it only if
// budgeted, then the `months` closed months before it -- skipping any month
// with no row rather than zero-filling it, exactly as the port's doc
// comment describes. Order is newest first because the walk itself runs
// newest to oldest.
func (r *fakeBudgetRepo) History(_ context.Context, householdID string, month time.Time, months int) ([]domain.Budget, error) {
	viewed := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	var out []domain.Budget
	if b, ok := r.budgets[budgetKey(householdID, viewed)]; ok {
		out = append(out, b)
	}
	for i := 1; i <= months; i++ {
		m := viewed.AddDate(0, -i, 0)
		if b, ok := r.budgets[budgetKey(householdID, m)]; ok {
			out = append(out, b)
		}
	}
	return out, nil
}

// RollOverToGoal mirrors the real one-transaction write the port's doc
// comment describes: stamp the month first, then write the contribution
// through r.goals, undoing the stamp if that write somehow fails -- so a
// test can never observe a stamped month with no contribution behind it, the
// exact strand DeleteContribution's own doc comment exists to prevent from
// the other direction. The stamp is checked and set before the write, not
// after, mirroring the real conditional UPDATE (... AND rolled_over_at IS
// NULL): a second call for the same month finds it already stamped and never
// reaches the contribution write at all, the same way a second concurrent
// UPDATE finds zero rows to touch.
func (r *fakeBudgetRepo) RollOverToGoal(ctx context.Context, in usecase.RollOverToGoalInput) (domain.GoalContribution, error) {
	month := time.Date(in.Month.Year(), in.Month.Month(), 1, 0, 0, 0, 0, time.UTC)
	key := budgetKey(in.HouseholdID, month)
	b, ok := r.budgets[key]
	if !ok {
		return domain.GoalContribution{}, domain.ErrNotFound
	}
	if _, done := r.rolledOver[key]; done {
		return domain.GoalContribution{}, domain.ErrRolloverAlreadyDone
	}
	r.rolledOver[key] = in.GoalID

	c, err := r.goals.AddContribution(ctx, domain.GoalContribution{
		GoalID:            in.GoalID,
		HouseholdID:       in.HouseholdID,
		Amount:            in.Amount,
		OccurredOn:        in.OccurredOn,
		Source:            domain.ContributionBudgetRollover,
		SourceBudgetMonth: &month,
	})
	if err != nil {
		delete(r.rolledOver, key)
		return domain.GoalContribution{}, err
	}

	// Stamp the stored row itself, not just the parallel rolledOver map, so
	// a caller reading it back through Get (BudgetService.Month does exactly
	// that) sees the same domain.Budget fields the real Postgres repository
	// would return after RollOverToGoal.
	stampedAt := in.OccurredOn
	b.RolledOverAt = &stampedAt
	b.RolloverGoalID = in.GoalID
	// Amount is frozen here, at write time -- the real Postgres repository's
	// Get reads it back off the goal_contributions row this same call wrote
	// (RollOverToGoal's own comment), never off a later recomputation. A
	// test that changes what this household-month's Remaining would compute
	// to AFTER this call (a late addExpense, most concretely) must still see
	// this exact figure back from Get -- that is the whole of what
	// TestBudgetMonthRolloverAmountSurvivesALaterTransaction pins.
	amount := in.Amount.Amount
	b.RolloverAmountMinor = &amount
	r.budgets[key] = b
	return c, nil
}

// --- GoalRepository --------------------------------------------------

// goalDouble is the in-memory GoalRepository every GoalService test (Task 6)
// and every BudgetService.RollOver test (Task 7) runs against -- the same
// map-backed shape the rest of this file uses. contributions is keyed by
// goal id; AddContribution only ever appends to its slice, so that slice's
// own order is a faithful creation-time record even though
// domain.GoalContribution itself carries no created_at column to sort by.
type goalDouble struct {
	goals         map[string]domain.Goal
	contributions map[string][]domain.GoalContribution // goal id -> contributions, in creation order
	n             int
	contribN      int

	// budgets, when set, is the fakeBudgetRepo whose rolledOver stamp
	// DeleteContribution clears when the removed row is a budget_rollover --
	// the reverse half of fakeBudgetRepo.setGoals's mutual reference. nil is
	// fine for a GoalService test that never touches a rollover
	// contribution; DeleteContribution then simply has nothing to clear.
	budgets *fakeBudgetRepo
}

func newGoalDouble() *goalDouble {
	return &goalDouble{goals: map[string]domain.Goal{}, contributions: map[string][]domain.GoalContribution{}}
}

// setBudgets completes fakeBudgetRepo.setGoals's mutual reference.
func (d *goalDouble) setBudgets(b *fakeBudgetRepo) { d.budgets = b }

// nameTaken reports whether name is already used by another goal in this
// household, archived rows included -- the same collision UNIQUE
// (household_id, name) enforces regardless of archived_at, and the same
// contract fakeCategoryRepo.nameTaken reproduces for categories. excludeID
// lets Update check every row but its own.
func (d *goalDouble) nameTaken(householdID, name, excludeID string) bool {
	for _, g := range d.goals {
		if g.HouseholdID == householdID && g.Name == name && g.ID != excludeID {
			return true
		}
	}
	return false
}

// contributedMinor sums every contribution a goal has -- starting balance
// and rollovers included -- which is GoalRecord.ContributedMinor, the "how
// much has actually accumulated" figure. MonthContributionTotals below
// answers a narrower question, "how much arrived this month," and excludes
// starting_balance for its own, separately load-bearing reason.
func (d *goalDouble) contributedMinor(goalID string) int64 {
	var total int64
	for _, c := range d.contributions[goalID] {
		total += c.Amount.Amount
	}
	return total
}

func (d *goalDouble) List(_ context.Context, householdID string, includeArchived bool) ([]usecase.GoalRecord, error) {
	var out []usecase.GoalRecord
	for _, g := range d.goals {
		if g.HouseholdID != householdID {
			continue
		}
		if g.IsArchived() && !includeArchived {
			continue
		}
		out = append(out, usecase.GoalRecord{Goal: g, ContributedMinor: d.contributedMinor(g.ID)})
	}
	// Dated goals first (newest TargetMonth first), then dateless goals last
	// -- the port's own doc comment pins this NULL placement explicitly, so
	// Task 4's ORDER BY cannot silently choose the other one. Name is the
	// tiebreak throughout: among dateless goals, and within an equal
	// TargetMonth, so the order is fully deterministic and no test can flake
	// on map iteration.
	sort.Slice(out, func(i, j int) bool {
		ti, tj := out[i].Goal.TargetMonth, out[j].Goal.TargetMonth
		switch {
		case ti == nil && tj == nil:
			return out[i].Goal.Name < out[j].Goal.Name
		case ti == nil:
			return false
		case tj == nil:
			return true
		case !ti.Equal(*tj):
			return ti.After(*tj)
		default:
			return out[i].Goal.Name < out[j].Goal.Name
		}
	})
	return out, nil
}

func (d *goalDouble) Get(_ context.Context, householdID, goalID string) (usecase.GoalRecord, error) {
	g, ok := d.goals[goalID]
	if !ok || g.HouseholdID != householdID {
		return usecase.GoalRecord{}, domain.ErrNotFound
	}
	return usecase.GoalRecord{Goal: g, ContributedMinor: d.contributedMinor(goalID)}, nil
}

// Create mirrors the real Create's own transaction: the goal row, then --
// only when startingBalanceMinor is non-zero -- its opening contribution,
// dated createdOn and sourced starting_balance. A zero startingBalanceMinor
// writes no contribution row at all, never a zero-amount one:
// goal_contributions' own CHECK (amount_minor <> 0) refuses a zero row on
// the real table, and this double must never attempt to write one either.
func (d *goalDouble) Create(_ context.Context, g domain.Goal, startingBalanceMinor int64, createdOn time.Time) (domain.Goal, error) {
	if d.nameTaken(g.HouseholdID, g.Name, "") {
		return domain.Goal{}, domain.ErrGoalNameTaken
	}
	d.n++
	g.ID = fmt.Sprintf("goal-%d", d.n)
	g.ArchivedAt = nil
	d.goals[g.ID] = g

	if startingBalanceMinor != 0 {
		d.contribN++
		d.contributions[g.ID] = append(d.contributions[g.ID], domain.GoalContribution{
			ID:          fmt.Sprintf("contribution-%d", d.contribN),
			GoalID:      g.ID,
			HouseholdID: g.HouseholdID,
			Amount:      domain.Money{Amount: startingBalanceMinor, Currency: g.Target.Currency},
			OccurredOn:  createdOn,
			Source:      domain.ContributionStartingBalance,
		})
	}
	return g, nil
}

// Update replaces name, target amount, target month and planned monthly --
// the port's own "every mutable column" list -- but never currency or
// ArchivedAt: a real UPDATE's SET list would simply omit both columns, so
// g.Target.Currency and g.ArchivedAt are read back off the existing row
// regardless of what the caller passed, rather than trusted from g.
// GoalService.Update is what refuses a currency change outright
// (domain.ErrGoalCurrencyImmutable) before this is ever reached; this double
// honours the immutability contract even if a future caller forgets to
// check, the same way a real SET list without a currency column would.
func (d *goalDouble) Update(_ context.Context, g domain.Goal) (domain.Goal, error) {
	existing, ok := d.goals[g.ID]
	if !ok || existing.HouseholdID != g.HouseholdID {
		return domain.Goal{}, domain.ErrNotFound
	}
	if d.nameTaken(g.HouseholdID, g.Name, g.ID) {
		return domain.Goal{}, domain.ErrGoalNameTaken
	}
	g.Target.Currency = existing.Target.Currency
	g.ArchivedAt = existing.ArchivedAt
	d.goals[g.ID] = g
	return g, nil
}

// SetArchived takes at as a parameter rather than reaching for time.Now()
// itself -- the port's own doc comment requires this, following
// AccountRepository.SetArchived's signature, so that today is always
// supplied by the caller (GoalService, reading its injected Clock) and never
// read from the wall clock inside a port implementation. It only stamps
// ArchivedAt the first time a goal is archived, so calling it again with
// archived=true is a true no-op that keeps the FIRST at rather than moving
// the timestamp forward -- the same idempotence CategoryRepository.SetArchived's
// own COALESCE(archived_at, now()) gives categories, adapted here for a
// caller-supplied timestamp instead of the database's now().
func (d *goalDouble) SetArchived(_ context.Context, householdID, goalID string, archived bool, at time.Time) (domain.Goal, error) {
	g, ok := d.goals[goalID]
	if !ok || g.HouseholdID != householdID {
		return domain.Goal{}, domain.ErrNotFound
	}
	if archived {
		if g.ArchivedAt == nil {
			stamp := at
			g.ArchivedAt = &stamp
		}
	} else {
		g.ArchivedAt = nil
	}
	d.goals[goalID] = g
	return g, nil
}

func (d *goalDouble) AddContribution(_ context.Context, c domain.GoalContribution) (domain.GoalContribution, error) {
	d.contribN++
	c.ID = fmt.Sprintf("contribution-%d", d.contribN)
	d.contributions[c.GoalID] = append(d.contributions[c.GoalID], c)
	return c, nil
}

// DeleteContribution scopes by household AND by goal, not by contribution id
// alone -- GoalRepository's own interface doc comment requires this, because
// 00007_goals.sql gives goal_contributions.household_id no database-level
// guarantee of agreeing with its own goal_id's household. When the removed
// row is a budget_rollover, it also clears that month's stamp on the paired
// fakeBudgetRepo (through d.budgets, wired by setBudgets) in the same call --
// the in-memory equivalent of the real one-transaction guarantee the port's
// doc comment describes; leaving the stamp would strand the household
// exactly as that comment warns.
func (d *goalDouble) DeleteContribution(_ context.Context, householdID, goalID, contributionID string) error {
	rows := d.contributions[goalID]
	for i, c := range rows {
		if c.ID != contributionID || c.HouseholdID != householdID {
			continue
		}
		d.contributions[goalID] = append(rows[:i:i], rows[i+1:]...)
		if c.Source == domain.ContributionBudgetRollover && d.budgets != nil && c.SourceBudgetMonth != nil {
			d.budgets.clearRolloverStamp(householdID, *c.SourceBudgetMonth)
		}
		return nil
	}
	return domain.ErrNotFound
}

// defaultContributionLimit and maxContributionLimit are ListContributions'
// own copy of TransactionRepository.List's clamp -- limit <= 0 becomes 50,
// anything above 200 is pulled down to it -- rather than a second, competing
// convention: a real SQL LIMIT 0 returns zero rows, the opposite of "no
// cap", so "no cap" was never a safe reading for this double to give limit
// <= 0 in the first place.
const (
	defaultContributionLimit = 50
	maxContributionLimit     = 200
)

// ListContributions reports newest first by OccurredOn, tie-broken by
// creation order (most recently added first): domain.GoalContribution
// carries no created_at the way the goal_contributions table does, so the
// double stands in for "created_at DESC" with the only ordering information
// it actually has -- AddContribution only ever appends, so the slice's own
// order already is chronological.
func (d *goalDouble) ListContributions(_ context.Context, householdID, goalID string, limit int) ([]domain.GoalContribution, error) {
	if limit <= 0 {
		limit = defaultContributionLimit
	} else if limit > maxContributionLimit {
		limit = maxContributionLimit
	}

	rows := d.contributions[goalID]
	ordered := make([]domain.GoalContribution, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- { // most recently added first, for the tiebreak below
		if rows[i].HouseholdID == householdID {
			ordered = append(ordered, rows[i])
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].OccurredOn.After(ordered[j].OccurredOn)
	})
	if len(ordered) > limit {
		ordered = ordered[:limit]
	}
	return ordered, nil
}

// MonthContributionTotals sums each unarchived goal's contributions inside
// one calendar month, excluding source starting_balance -- the exclusion the
// port's own doc comment calls load-bearing. An archived goal is left out of
// the result entirely, matching "sums each unarchived goal's contributions."
// The result is sorted by goal id purely for test determinism; nothing in
// the port promises an order.
func (d *goalDouble) MonthContributionTotals(_ context.Context, householdID string, month time.Time) ([]usecase.GoalMonthTotal, error) {
	totals := map[string]int64{}
	for goalID, rows := range d.contributions {
		g, ok := d.goals[goalID]
		if !ok || g.HouseholdID != householdID || g.IsArchived() {
			continue
		}
		for _, c := range rows {
			if c.HouseholdID != householdID {
				continue
			}
			if c.Source == domain.ContributionStartingBalance {
				continue // load-bearing exclusion -- see the port's own doc comment
			}
			if c.OccurredOn.Year() != month.Year() || c.OccurredOn.Month() != month.Month() {
				continue
			}
			totals[goalID] += c.Amount.Amount
		}
	}
	out := make([]usecase.GoalMonthTotal, 0, len(totals))
	for goalID, amount := range totals {
		out = append(out, usecase.GoalMonthTotal{GoalID: goalID, AmountMinor: amount})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GoalID < out[j].GoalID })
	return out, nil
}

// var _ usecase.GoalRepository = (*goalDouble)(nil) below is load-bearing,
// not decoration: nothing in internal/usecase constructs a GoalService yet
// (that is Task 6's job), so without this assertion a signature drift
// between this double and ports.go's GoalRepository would not surface until
// then -- the same reasoning convert.go's own compile-time repository
// assertions give for the postgres adapters.
var _ usecase.GoalRepository = (*goalDouble)(nil)

// staticTestRates knows the one pair fx.StaticProvider knows, and errors on
// everything else -- so a test for the no-rate branch does not have to invent
// a second, differently-behaved double.
type staticTestRates struct{}

func (staticTestRates) Rate(_ context.Context, from, to string) (usecase.Rate, error) {
	switch {
	case from == to:
		return usecase.Rate{Numerator: 1, Denominator: 1}, nil
	case from == "SGD" && to == "IDR":
		return usecase.Rate{Numerator: 12_410, Denominator: 1}, nil
	case from == "IDR" && to == "SGD":
		return usecase.Rate{Numerator: 1, Denominator: 12_410}, nil
	default:
		return usecase.Rate{}, fmt.Errorf("no rate available for %s to %s", from, to)
	}
}

// --- Transactions ------------------------------------------------------

type fakeTransactionRepo struct {
	transactions []domain.Transaction
	nextID       int

	// beforeFromOpening is the set of transaction ids MonthTotals reports as
	// dated before their from-account's opening balance -- the same
	// BeforeFromAccountOpening flag the real repository computes via a join
	// to Account.OpeningBalanceAsOf (see transaction_repo.go). This fake has
	// no accounts to join against, so a test that needs the flag set marks it
	// directly with markBeforeFromAccountOpening rather than the fake
	// inferring it from a date comparison it cannot actually make.
	beforeFromOpening map[string]bool
}

// markBeforeFromAccountOpening flags transactionID so MonthTotals reports its
// BeforeFromAccountOpening as true, for a test proving that a transaction the
// balance ignores still counts toward spend (decision 6).
func (f *fakeTransactionRepo) markBeforeFromAccountOpening(transactionID string) {
	if f.beforeFromOpening == nil {
		f.beforeFromOpening = map[string]bool{}
	}
	f.beforeFromOpening[transactionID] = true
}

func (f *fakeTransactionRepo) Create(_ context.Context, t domain.Transaction) (domain.Transaction, error) {
	f.nextID++
	t.ID = fmt.Sprintf("txn-%d", f.nextID)
	f.transactions = append(f.transactions, t)
	return t, nil
}

func (f *fakeTransactionRepo) Get(_ context.Context, householdID, id string) (usecase.TransactionView, error) {
	for _, t := range f.transactions {
		if t.ID == id && t.HouseholdID == householdID {
			return usecase.TransactionView{Transaction: t}, nil
		}
	}
	return usecase.TransactionView{}, domain.ErrNotFound
}

func (f *fakeTransactionRepo) Update(_ context.Context, t domain.Transaction) (domain.Transaction, error) {
	for i, existing := range f.transactions {
		if existing.ID == t.ID {
			f.transactions[i] = t
			return t, nil
		}
	}
	return domain.Transaction{}, domain.ErrNotFound
}

func (f *fakeTransactionRepo) Delete(_ context.Context, householdID, id string) error {
	for i, t := range f.transactions {
		if t.ID == id && t.HouseholdID == householdID {
			f.transactions = append(f.transactions[:i], f.transactions[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (f *fakeTransactionRepo) List(_ context.Context, householdID string, _ usecase.TransactionFilter) ([]usecase.TransactionView, error) {
	out := []usecase.TransactionView{}
	for _, t := range f.transactions {
		if t.HouseholdID == householdID {
			out = append(out, usecase.TransactionView{Transaction: t})
		}
	}
	return out, nil
}

func (f *fakeTransactionRepo) MonthTotals(_ context.Context, householdID string, month time.Time) ([]usecase.TransactionView, error) {
	out := []usecase.TransactionView{}
	for _, t := range f.transactions {
		if t.HouseholdID != householdID {
			continue
		}
		if t.OccurredOn.Year() == month.Year() && t.OccurredOn.Month() == month.Month() {
			view := usecase.TransactionView{Transaction: t}
			if f.beforeFromOpening[t.ID] {
				before := true
				view.BeforeFromAccountOpening = &before
			}
			out = append(out, view)
		}
	}
	return out, nil
}

// fakeCategoryLookup answers the two questions validation asks, and nothing
// else -- the narrow port it stands in for.
type fakeCategoryLookup struct {
	kinds map[string]domain.CategoryKind // category id -> kind, for one household
}

func (f *fakeCategoryLookup) BelongsToHousehold(_ context.Context, _, categoryID string) (bool, error) {
	_, ok := f.kinds[categoryID]
	return ok, nil
}

func (f *fakeCategoryLookup) Kind(_ context.Context, _, categoryID string) (domain.CategoryKind, error) {
	kind, ok := f.kinds[categoryID]
	if !ok {
		return "", domain.ErrNotFound
	}
	return kind, nil
}

// fakeAccountRecord is one account's currency and the household it actually
// belongs to. archived, added in Task 6 for BillService's own archived-account
// guards (Create's pay-from check, MarkPaid's in Task 7), defaults to false so
// every existing fakeAccountRecord{...} literal elsewhere in this package --
// none of which name the field -- is unaffected.
type fakeAccountRecord struct {
	householdID string
	currency    string
	archived    bool
}

// fakeAccountLookup holds accounts keyed by id, each carrying its own
// household. Get refuses both an id it has never heard of and one that
// belongs to a household other than the one asked about -- the same collapse
// AccountLookup.Get's contract requires, so a test can plant an account that
// genuinely exists, just not here, rather than relying on an unknown id to
// stand in for that case.
type fakeAccountLookup struct {
	accounts    map[string]fakeAccountRecord
	memberships map[string]string // membership id -> owning household
}

// archivedStamp is the fixed ArchivedAt every archived fakeAccountRecord
// gets. No test reads its value, only its non-nilness (domain.Account.
// IsArchived), so a fixed constant is used rather than time.Now() -- a test
// double has no more business reading the wall clock than the service it
// stands in for.
var archivedStamp = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

func (f *fakeAccountLookup) Get(_ context.Context, householdID, accountID string) (usecase.AccountView, error) {
	a, ok := f.accounts[accountID]
	if !ok || a.householdID != householdID {
		return usecase.AccountView{}, domain.ErrNotFound
	}
	acct := domain.Account{
		ID:             accountID,
		HouseholdID:    a.householdID,
		OpeningBalance: domain.Money{Currency: a.currency},
	}
	if a.archived {
		stamp := archivedStamp
		acct.ArchivedAt = &stamp
	}
	return usecase.AccountView{
		Account: acct,
		Balance: domain.Money{Currency: a.currency},
	}, nil
}

func (f *fakeAccountLookup) MembershipBelongsToHousehold(_ context.Context, householdID, membershipID string) (bool, error) {
	return f.memberships[membershipID] == householdID, nil
}

// --- BillRepository ------------------------------------------------------

// fakeBillRepo implements usecase.BillRepository entirely in memory. add
// assigns a sequential id ("bill-1", "bill-2", ...) to any record arriving
// with no id of its own, mirroring how the postgres adapter's INSERT assigns
// one -- Task 6's own fixtures rely on this, and so does Task 7's
// repo.add(bill(...)) followed by a literal "bill-1" in the same test.
//
// RecordPayment and UndoPayment are deliberately minimal: Task 6 does not
// implement BillService.MarkPaid/UndoPayment (that is Task 7's job), so
// these two exist only to satisfy the interface and to give Task 7 something
// to extend -- RecordPayment records lastWrite and advances the matching
// bill's NextDue but does not yet enforce the real port's ErrAlreadyExists
// duplicate-occurrence rule, and UndoPayment is a plain delete rather than
// the real port's most-recent-only guard (undoErr lets a test force that
// refusal directly instead).
type fakeBillRepo struct {
	records  []usecase.BillRecord
	payments []usecase.BillPaymentRecord
	n, payN  int

	// err, when set, is returned unconditionally by every method below
	// except RecordPayment/UndoPayment, which have their own dedicated
	// force-failure hooks -- a single shared err would make "the list call
	// failed" and "the payment call failed" indistinguishable to a test that
	// armed it.
	err error

	// lastWrite is the PaymentWrite RecordPayment most recently received --
	// Task 7's MarkPaid assertions read this directly, since it is what the
	// SERVICE assembled and is the thing actually worth pinning down.
	lastWrite usecase.PaymentWrite

	// undoErr, when set, is what UndoPayment returns unconditionally, letting
	// a test force the most-recent-only refusal (or any other repository
	// failure) without needing two real payments to construct it.
	undoErr error
}

// add appends rec, assigning a "bill-N" id when rec.Bill.ID is empty and "h1"
// as the household when rec.Bill.HouseholdID is empty -- every fixture
// helper in bill_test.go already sets HouseholdID, but leaving this fallback
// costs nothing and matches goalDouble.Create's own "assign what a real
// INSERT would" convention.
func (r *fakeBillRepo) add(rec usecase.BillRecord) usecase.BillRecord {
	if rec.Bill.ID == "" {
		r.n++
		rec.Bill.ID = fmt.Sprintf("bill-%d", r.n)
	}
	if rec.Bill.HouseholdID == "" {
		rec.Bill.HouseholdID = "h1"
	}
	r.records = append(r.records, rec)
	return rec
}

func (r *fakeBillRepo) List(_ context.Context, householdID string, includeArchived bool) ([]usecase.BillRecord, error) {
	if r.err != nil {
		return nil, r.err
	}
	var out []usecase.BillRecord
	for _, rec := range r.records {
		if rec.Bill.HouseholdID != householdID {
			continue
		}
		if rec.Bill.IsArchived() && !includeArchived {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

func (r *fakeBillRepo) Get(_ context.Context, householdID, billID string) (usecase.BillRecord, error) {
	if r.err != nil {
		return usecase.BillRecord{}, r.err
	}
	for _, rec := range r.records {
		if rec.Bill.ID == billID && rec.Bill.HouseholdID == householdID {
			return rec, nil
		}
	}
	return usecase.BillRecord{}, domain.ErrNotFound
}

// nameTaken mirrors goalDouble.nameTaken/fakeCategoryRepo.nameTaken for
// bills: a name already used in this household, archived rows included,
// collides -- the same UNIQUE (household_id, name) contract BillRepository's
// own doc comment states for Create and Update alike.
func (r *fakeBillRepo) nameTaken(householdID, name, excludeID string) bool {
	for _, rec := range r.records {
		if rec.Bill.HouseholdID == householdID && rec.Bill.Name == name && rec.Bill.ID != excludeID {
			return true
		}
	}
	return false
}

// Create mirrors BillRepository.Create's own contract, with one known gap:
// BillRecord.Amount.Currency is meant to come from the pay-from account's
// join (that type's own doc comment), but this double holds no accounts
// table to join against -- NewBillRow itself carries no currency for Create
// to fall back on either. No test in this package's suite reads the
// resulting Amount.Currency, so the gap is left undocumented-but-real rather
// than papered over with a guess; a future test that needs it should wire
// this double to the fakeAccountLookup it is already built alongside.
func (r *fakeBillRepo) Create(_ context.Context, in usecase.NewBillRow) (usecase.BillRecord, error) {
	if r.err != nil {
		return usecase.BillRecord{}, r.err
	}
	if r.nameTaken(in.HouseholdID, in.Name, "") {
		return usecase.BillRecord{}, domain.ErrBillNameTaken
	}
	due := in.NextDue
	rec := usecase.BillRecord{
		Bill: domain.Bill{
			HouseholdID:        in.HouseholdID,
			Name:               in.Name,
			Amount:             domain.Money{Amount: in.AmountMinor},
			Cadence:            in.Cadence,
			NextDue:            &due,
			DueAnchorDay:       in.DueAnchorDay,
			CategoryID:         in.CategoryID,
			PayFromAccountID:   in.PayFromAccountID,
			PaidByMembershipID: in.PaidByMembershipID,
			Autopay:            in.Autopay,
			IsSubscription:     in.IsSubscription,
		},
	}
	return r.add(rec), nil
}

// Update mirrors BillRepository.Update: it replaces b wholesale. CategoryName
// and AccountName are carried over from the existing record unchanged, since
// b (a domain.Bill) carries no names of its own to promote into them -- the
// same "read the names back off the existing row" contract goalDouble.Update
// follows for a currency neither side is allowed to touch.
func (r *fakeBillRepo) Update(_ context.Context, b domain.Bill) (usecase.BillRecord, error) {
	if r.err != nil {
		return usecase.BillRecord{}, r.err
	}
	for i, rec := range r.records {
		if rec.Bill.ID != b.ID || rec.Bill.HouseholdID != b.HouseholdID {
			continue
		}
		if r.nameTaken(b.HouseholdID, b.Name, b.ID) {
			return usecase.BillRecord{}, domain.ErrBillNameTaken
		}
		r.records[i].Bill = b
		return r.records[i], nil
	}
	return usecase.BillRecord{}, domain.ErrNotFound
}

// SetArchived only stamps ArchivedAt the first time a bill is archived,
// mirroring goalDouble.SetArchived's own idempotence: calling it again with
// archived=true keeps the FIRST at rather than moving the timestamp forward.
func (r *fakeBillRepo) SetArchived(_ context.Context, householdID, billID string, archived bool, at time.Time) (usecase.BillRecord, error) {
	if r.err != nil {
		return usecase.BillRecord{}, r.err
	}
	for i, rec := range r.records {
		if rec.Bill.ID != billID || rec.Bill.HouseholdID != householdID {
			continue
		}
		if archived {
			if rec.Bill.ArchivedAt == nil {
				stamp := at
				r.records[i].Bill.ArchivedAt = &stamp
			}
		} else {
			r.records[i].Bill.ArchivedAt = nil
		}
		return r.records[i], nil
	}
	return usecase.BillRecord{}, domain.ErrNotFound
}

// RecordPayment -- see this type's own doc comment for what it deliberately
// does not yet do.
func (r *fakeBillRepo) RecordPayment(_ context.Context, in usecase.PaymentWrite) (usecase.BillPaymentRecord, error) {
	if r.err != nil {
		return usecase.BillPaymentRecord{}, r.err
	}
	r.lastWrite = in
	r.payN++
	pay := domain.BillPayment{
		ID:            fmt.Sprintf("pay-%d", r.payN),
		BillID:        in.BillID,
		HouseholdID:   in.HouseholdID,
		DueOn:         in.DueOn,
		PaidOn:        in.PaidOn,
		Amount:        domain.Money{Amount: in.AmountMinor, Currency: in.Currency},
		TransactionID: fmt.Sprintf("txn-%d", r.payN),
	}
	rec := usecase.BillPaymentRecord{Payment: pay, BillName: in.Description}
	r.payments = append(r.payments, rec)

	for i, existing := range r.records {
		if existing.Bill.ID == in.BillID && existing.Bill.HouseholdID == in.HouseholdID {
			r.records[i].Bill.NextDue = in.NextDue
		}
	}
	return rec, nil
}

// UndoPayment -- see this type's own doc comment for what it deliberately
// does not yet do.
func (r *fakeBillRepo) UndoPayment(_ context.Context, householdID, billID, paymentID string) error {
	if r.undoErr != nil {
		return r.undoErr
	}
	for i, p := range r.payments {
		if p.Payment.ID == paymentID && p.Payment.BillID == billID && p.Payment.HouseholdID == householdID {
			r.payments = append(r.payments[:i], r.payments[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *fakeBillRepo) ListPayments(_ context.Context, householdID string, month time.Time) ([]usecase.BillPaymentRecord, error) {
	if r.err != nil {
		return nil, r.err
	}
	var out []usecase.BillPaymentRecord
	for _, p := range r.payments {
		if p.Payment.HouseholdID != householdID {
			continue
		}
		if p.Payment.DueOn.Year() == month.Year() && p.Payment.DueOn.Month() == month.Month() {
			out = append(out, p)
		}
	}
	// Newest paid_on first, ties by bill name -- BillRepository.ListPayments'
	// own contract.
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Payment.PaidOn.Equal(out[j].Payment.PaidOn) {
			return out[i].Payment.PaidOn.After(out[j].Payment.PaidOn)
		}
		return out[i].BillName < out[j].BillName
	})
	return out, nil
}

// MonthTotals reproduces the port's own union rule (see BillRepository's
// header comment): dueMinor is every unarchived bill's amount still due in
// month, PLUS every payment due in month; paidMinor is payments due in month
// alone. Both are keyed by currency -- bills.Amount.Currency (via its
// pay-from account) is the only currency this double, like the real schema,
// ever carries.
func (r *fakeBillRepo) MonthTotals(_ context.Context, householdID string, month time.Time) (map[string]int64, map[string]int64, error) {
	if r.err != nil {
		return nil, nil, r.err
	}
	dueMinor := map[string]int64{}
	paidMinor := map[string]int64{}

	for _, rec := range r.records {
		b := rec.Bill
		if b.HouseholdID != householdID || b.IsArchived() || b.NextDue == nil {
			continue
		}
		if b.NextDue.Year() == month.Year() && b.NextDue.Month() == month.Month() {
			dueMinor[b.Amount.Currency] += b.Amount.Amount
		}
	}
	for _, p := range r.payments {
		if p.Payment.HouseholdID != householdID {
			continue
		}
		if p.Payment.DueOn.Year() == month.Year() && p.Payment.DueOn.Month() == month.Month() {
			paidMinor[p.Payment.Amount.Currency] += p.Payment.Amount.Amount
			dueMinor[p.Payment.Amount.Currency] += p.Payment.Amount.Amount
		}
	}
	return dueMinor, paidMinor, nil
}

// var _ usecase.BillRepository = (*fakeBillRepo)(nil) below is load-bearing,
// not decoration -- see goalDouble's own identical assertion for why.
var _ usecase.BillRepository = (*fakeBillRepo)(nil)

// --- RetroRepository ---------------------------------------------------

// retroRow is one retro as the double stores it: RetroRecord plus the
// household scope, which RetroRecord itself has no field for (the same
// split goalDouble's own map -- keyed data, HouseholdID read off the
// embedded domain.Goal -- draws for a different reason; RetroRecord embeds
// no domain type to carry it, so the row keeps it alongside instead).
type retroRow struct {
	usecase.RetroRecord
	HouseholdID string
}

// retroRepoDouble is the in-memory RetroRepository every RetroService test
// runs against -- Task 3's own List/Month tests, and Task 4's write-path
// tests reusing this same double. It implements every method the port
// declares, including the ones Task 3's tests never call (Create, Update,
// Complete, DeleteDraft): a double that only satisfies the methods its own
// author happened to test is not honouring the port's whole contract
// (Liskov, CLAUDE.md), and Task 4 needs the rest of this double to already
// behave correctly when it arrives.
//
// Every seeded and created row lives under householdID "hh" -- the brief's
// own test fixtures never pass a household id into seed, so this is the one
// value every test in this package actually uses; List/ByMonth/etc still
// filter on it explicitly, the same as a real repository would for any
// other household id.
type retroRepoDouble struct {
	rows map[string]*retroRow // keyed by id
	n    int

	// writes counts every call that mutates a row -- Create, Update,
	// Complete, DeleteDraft -- the same role userDouble.count and
	// sessionDouble.count play for their own repositories. Task 3's own
	// tests never touch it (List and Month write nothing); it is added now
	// because Task 4's save/finish/discard-draft tests need a way to assert
	// nothing was written on a refused call, and this is where the double
	// they will reuse already lives.
	writes int
}

func newRetroRepoDouble() *retroRepoDouble {
	return &retroRepoDouble{rows: map[string]*retroRow{}}
}

// seed inserts a retro directly, bypassing Create, for a test that wants one
// to already exist. mood == 0 means "nobody has picked one" (RetroRecord.Mood
// is a pointer for exactly this reason -- 0 is not a mood); any other value
// is stored as that mood, draft or finished alike, so a test can seed a
// draft that already carries a mood (decision 2's own scenario: a mood is
// picked before the rest of the retro is finished). finished stamps
// CompletedAt at the retro's own month -- an arbitrary but deterministic
// instant; no test in this package reads it -- and leaves a draft's
// CompletedAt nil, the same state Create leaves behind.
func (d *retroRepoDouble) seed(month time.Time, mood int, notes string, finished bool) usecase.RetroRecord {
	d.n++
	row := &retroRow{
		RetroRecord: usecase.RetroRecord{
			ID:      fmt.Sprintf("retro-%d", d.n),
			Month:   month,
			Notes:   notes,
			Version: 1,
		},
		HouseholdID: "hh",
	}
	if mood != 0 {
		m := mood
		row.Mood = &m
	}
	if finished {
		at := month
		row.CompletedAt = &at
	}
	d.rows[row.ID] = row
	return row.RetroRecord
}

// Create mirrors the real Create's own UNIQUE (household_id, month)
// constraint: a second Create for a month that already has a row reports
// domain.ErrAlreadyExists rather than overwriting it, which is what makes a
// double-clicked "Start retro" button harmless in the real adapter too.
func (d *retroRepoDouble) Create(_ context.Context, householdID string, month time.Time) (usecase.RetroRecord, error) {
	for _, row := range d.rows {
		if row.HouseholdID == householdID && row.Month.Equal(month) {
			return usecase.RetroRecord{}, domain.ErrAlreadyExists
		}
	}
	d.n++
	d.writes++
	row := &retroRow{
		RetroRecord: usecase.RetroRecord{ID: fmt.Sprintf("retro-%d", d.n), Month: month, Version: 1},
		HouseholdID: householdID,
	}
	d.rows[row.ID] = row
	return row.RetroRecord, nil
}

// ByMonth reports domain.ErrNotFound when the month has no retro -- the
// port's own contract, and the distinction RetroService.Month relies on to
// tell "not started" apart from a real failure.
func (d *retroRepoDouble) ByMonth(_ context.Context, householdID string, month time.Time) (usecase.RetroRecord, error) {
	for _, row := range d.rows {
		if row.HouseholdID == householdID && row.Month.Equal(month) {
			return row.RetroRecord, nil
		}
	}
	return usecase.RetroRecord{}, domain.ErrNotFound
}

// List returns every retro for householdID, newest month first -- the
// port's own ordering contract, which RetroService.List relies on rather
// than re-sorting. ActionCount is always 0: nothing in this package wires a
// retroRepoDouble to a retroActionRepoDouble's rows, and no test here reads
// it -- a real adapter's join is what actually supplies this figure.
func (d *retroRepoDouble) List(_ context.Context, householdID string) ([]usecase.RetroSummary, error) {
	var out []usecase.RetroSummary
	for _, row := range d.rows {
		if row.HouseholdID != householdID {
			continue
		}
		out = append(out, usecase.RetroSummary{Retro: row.RetroRecord})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Retro.Month.After(out[j].Retro.Month) })
	return out, nil
}

// Update mirrors the real repository's guarded UPDATE: a row is matched by
// id, household AND month (RetroUpdate.Month's own doc comment explains
// why -- a zero-row match on id+household alone cannot tell "no such retro"
// apart from "the version moved," so the real adapter re-reads ByMonth to
// decide, reproduced here by keying the match on Month directly). A version
// that no longer matches the stored one reports domain.ErrRetroChanged and
// writes nothing; the returned record always carries the NEW version.
func (d *retroRepoDouble) Update(_ context.Context, u usecase.RetroUpdate) (usecase.RetroRecord, error) {
	row, ok := d.rows[u.RetroID]
	if !ok || row.HouseholdID != u.HouseholdID || !row.Month.Equal(u.Month) {
		return usecase.RetroRecord{}, domain.ErrNotFound
	}
	if row.Version != u.Version {
		return usecase.RetroRecord{}, domain.ErrRetroChanged
	}
	d.writes++
	row.Mood = u.Mood
	row.WentWell = u.WentWell
	row.WasHard = u.WasHard
	row.Notes = u.Notes
	row.Version++
	return row.RetroRecord, nil
}

// Complete stamps CompletedAt with at, idempotently: completing an
// already-finished retro leaves the FIRST stamp rather than moving it
// forward, the same idempotence goalDouble.SetArchived gives archiving.
func (d *retroRepoDouble) Complete(_ context.Context, householdID, retroID string, at time.Time) (usecase.RetroRecord, error) {
	row, ok := d.rows[retroID]
	if !ok || row.HouseholdID != householdID {
		return usecase.RetroRecord{}, domain.ErrNotFound
	}
	d.writes++
	if row.CompletedAt == nil {
		stamp := at
		row.CompletedAt = &stamp
	}
	return row.RetroRecord, nil
}

// DeleteDraft mirrors the real repository's WHERE ... AND completed_at IS
// NULL: a finished retro's zero-row match reports domain.ErrNotFound, the
// same as a retro that never existed -- deleting a draft is not this
// double's job to allow just because the id and household matched.
func (d *retroRepoDouble) DeleteDraft(_ context.Context, householdID, retroID string) error {
	row, ok := d.rows[retroID]
	if !ok || row.HouseholdID != householdID || row.CompletedAt != nil {
		return domain.ErrNotFound
	}
	d.writes++
	delete(d.rows, retroID)
	return nil
}

// var _ usecase.RetroRepository = (*retroRepoDouble)(nil) below is
// load-bearing, not decoration -- see goalDouble's own identical assertion
// for why.
var _ usecase.RetroRepository = (*retroRepoDouble)(nil)

// --- RetroActionRepository ----------------------------------------------

// retroActionRow is one action as the double stores it: RetroActionRecord
// plus the household scope and the month it belongs to. RetroActionRecord
// itself carries neither -- only RetroID -- so a real repository answers
// OpenInMonth by joining retro_actions back to retros for the month; this
// double keeps that joined fact on the row directly rather than reaching
// into a retroRepoDouble it is not guaranteed to share a test with.
type retroActionRow struct {
	usecase.RetroActionRecord
	HouseholdID string
	Month       time.Time
}

// retroActionRepoDouble is the in-memory RetroActionRepository every
// RetroService test runs against, implementing every port method for the
// same Liskov reason retroRepoDouble does.
type retroActionRepoDouble struct {
	rows map[string]*retroActionRow
	n    int
}

func newRetroActionRepoDouble() *retroActionRepoDouble {
	return &retroActionRepoDouble{rows: map[string]*retroActionRow{}}
}

// seedOpen inserts an open (unticked) action against month, for the
// "Still open from July" fixture OpenInMonth answers. Household "hh",
// matching retroRepoDouble.seed's own fixed household -- see that method's
// comment for why.
func (d *retroActionRepoDouble) seedOpen(month time.Time, body string) usecase.RetroActionRecord {
	d.n++
	row := &retroActionRow{
		RetroActionRecord: usecase.RetroActionRecord{ID: fmt.Sprintf("action-%d", d.n), Body: body},
		HouseholdID:       "hh",
		Month:             month,
	}
	d.rows[row.ID] = row
	return row.RetroActionRecord
}

// Add writes one action. It does not resolve which month the action's own
// retro belongs to (RetroActionInput carries a RetroID, not a month, and
// this double is never wired to a retroRepoDouble to look one up) -- an
// action added this way therefore never appears in OpenInMonth, only
// through ForRetro. Task 3's tests reach OpenInMonth exclusively through
// seedOpen, which sets Month directly, so this gap is Task 4's to close if
// a later test needs Add and OpenInMonth to agree.
func (d *retroActionRepoDouble) Add(_ context.Context, in usecase.RetroActionInput) (usecase.RetroActionRecord, error) {
	d.n++
	row := &retroActionRow{
		RetroActionRecord: usecase.RetroActionRecord{
			ID:                    fmt.Sprintf("action-%d", d.n),
			RetroID:               in.RetroID,
			Body:                  in.Body,
			CarriedFrom:           in.CarriedFrom,
			AssigneeMembershipIDs: in.AssigneeMembershipIDs,
		},
		HouseholdID: in.HouseholdID,
	}
	d.rows[row.ID] = row
	return row.RetroActionRecord, nil
}

// ForRetro returns a retro's actions in insertion order -- map iteration
// order is not that, so this sorts by id, which encodes creation order for
// every row this double ever produces (both seedOpen and Add assign
// "action-N" with N increasing).
func (d *retroActionRepoDouble) ForRetro(_ context.Context, householdID, retroID string) ([]usecase.RetroActionRecord, error) {
	var out []usecase.RetroActionRecord
	for _, row := range d.rows {
		if row.HouseholdID == householdID && row.RetroID == retroID {
			out = append(out, row.RetroActionRecord)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// SetDone ticks or unticks, clearing DoneAt on done=false rather than
// stamping a "not done" time -- the port's own contract -- and reports
// domain.ErrNotFound on a zero-row match.
func (d *retroActionRepoDouble) SetDone(_ context.Context, householdID, actionID string, done bool, at time.Time) error {
	row, ok := d.rows[actionID]
	if !ok || row.HouseholdID != householdID {
		return domain.ErrNotFound
	}
	if done {
		stamp := at
		row.DoneAt = &stamp
	} else {
		row.DoneAt = nil
	}
	return nil
}

// Remove hard-deletes an action, reporting domain.ErrNotFound on a zero-row
// match -- the same convention TransactionRepository.Delete and
// GoalRepository.DeleteContribution use for their own unqualified deletes.
func (d *retroActionRepoDouble) Remove(_ context.Context, householdID, actionID string) error {
	row, ok := d.rows[actionID]
	if !ok || row.HouseholdID != householdID {
		return domain.ErrNotFound
	}
	delete(d.rows, actionID)
	return nil
}

// OpenInMonth returns month's unticked actions -- the "Still open from
// July" offer -- scoped to exactly that month, never a range: a household
// that skipped months must not be handed an unbounded backlog (spec
// decision 4).
func (d *retroActionRepoDouble) OpenInMonth(_ context.Context, householdID string, month time.Time) ([]usecase.RetroActionRecord, error) {
	var out []usecase.RetroActionRecord
	for _, row := range d.rows {
		if row.HouseholdID == householdID && row.Month.Equal(month) && row.DoneAt == nil {
			out = append(out, row.RetroActionRecord)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// var _ usecase.RetroActionRepository = (*retroActionRepoDouble)(nil) below
// is load-bearing, not decoration -- see goalDouble's own identical
// assertion for why.
var _ usecase.RetroActionRepository = (*retroActionRepoDouble)(nil)
