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

	// byEmailCalls counts ByEmail invocations. RequestMagicLink is meant to
	// call this exactly once per request regardless of outcome (see
	// magicLinkDouble.countSinceCalls for the matching count on the other
	// read) -- this field is how a test pins that down without resorting to
	// wall-clock timing. It is safe unguarded because every test that reads
	// it calls AuthService methods synchronously from the test goroutine
	// itself; only the mailer double's state is touched from a background
	// goroutine.
	byEmailCalls int

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

// --- Mailer ---------------------------------------------------------

type sentMail struct {
	To   string
	Name string
	URL  string
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
	mu         sync.Mutex
	magicLinks []sentMail
	invites    []sentMail
	failNext   error
	panicNext  string
	sent       chan struct{}
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

// Create mirrors CreateHousehold's schema defaults (migrations/00002_identity.sql):
// SGD primary, IDR secondary, shown, auto FX mode.
func (d *householdDouble) Create(_ context.Context, name, familyName string) (domain.Household, error) {
	d.n++
	h := domain.Household{
		ID: fmt.Sprintf("household-%d", d.n), Name: name, FamilyName: familyName,
		PrimaryCurrency: "SGD", ShowSecondaryCurrency: true, SecondaryCurrency: "IDR", FXRateMode: "auto",
	}
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
