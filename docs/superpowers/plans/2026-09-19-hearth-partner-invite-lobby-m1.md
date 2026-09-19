# Partner Invite Lobby, Milestone 1 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Owners can see every invite their household has sent that nobody has accepted, withdraw one, and are asked to "Invite your partner" on Overview's setup checklist.

**Architecture:** Two new `InviteRepository` methods (`ListPending`, `Delete`), exposed through `InviteService.ListPending` and `InviteService.Withdraw`. Two routes: `GET /household/invites` (owner) and `DELETE /household/invites/{id}` (owner, CSRF, browser session). The frontend adds a `PendingInvitesList` inside Settings' Members panel and a fourth checklist step on Overview. **No schema change.** Milestone 2 (the Telegram link, knock and Let in) is a separate plan, written once this one is merged, because its code builds on the names this plan creates.

**Tech Stack:** Go 1.25 (chi, pgx, sqlc), Postgres, React 19 + TypeScript, TanStack Query and Router, zod 4, vitest.

**Spec:** `docs/superpowers/specs/2026-09-19-hearth-partner-invite-lobby-design.md` (decisions 12 and 13; Data "Pending" definition; API rows marked M1; Frontend "Pending section" and "SetupChecklist"). PRD: `.claude/prds/partner-invite-lobby.prd.md`, milestone 1.

## Global Constraints

- `go` is not on `PATH` in a bare shell. Before any `go` or `make` command: `export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH`.
- The Go suite needs Docker: `export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock` and `export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`.
- `make lint-arch` enforces layering: `internal/domain` imports only the standard library; `internal/usecase` may add `internal/domain`; no `pgx` or HTTP type leaves `internal/adapter/**`. A missing row is `domain.ErrNotFound` above the adapter.
- **Pending** means `accepted_at IS NULL AND expires_at > now`, with `now` supplied by the caller's `Clock`. One definition everywhere (spec, Data).
- **Withdraw deletes the row**, scoped by household in the SQL itself (spec decision 13). An accepted invite is refused with `domain.ErrInviteAlreadyAccepted`; another household's id is `domain.ErrNotFound`.
- **Every 2xx except 204 carries a JSON body.** An empty list is `[]`, never `null`.
- **Authorisation lives only in the HTTP layer.** Services take no actor parameter.
- Withdraw needs `requireCookieSession` (spec decision 12). Listing does not: it shows no secret.
- Frontend query keys come from exported builders, never literals. `pendingInvitesQueryKey` starts with `"household"`, because `PATCH /household` invalidates by that prefix (`useHouseholdMembers.ts`' header).
- `stubFetchRoutes` (`web/src/test/fetchStub.ts`) **throws on any request it has no route for.** A component that starts fetching a new URL breaks every existing test that renders it until that route is registered.
- Comments say **why**, never what the line already says. Exported things carry their contract in a doc comment.
- Do **not** add `channel` or `knock` anywhere. They are milestone 2.
- Every commit message ends with the line `Claude-Session: https://claude.ai/code/session_01EVA25nn5hrgVDeR3C1m8zA`.

---

### Task 1: Repository: list pending invites and delete one

**Files:**
- Modify: `api/internal/adapter/postgres/queries/identity.sql` (after `MarkInviteAccepted`, ~line 161)
- Regenerate: `api/internal/adapter/postgres/sqlcgen/` (via `make sqlc`, never by hand)
- Modify: `api/internal/usecase/ports.go` (`InviteDetails` ~line 561 and `InviteRepository` ~line 581)
- Modify: `api/internal/adapter/postgres/invite_repo.go`
- Modify: `api/internal/usecase/testdouble_test.go` (`inviteRow` ~line 681, `inviteDouble` ~line 702)
- Test: `api/internal/adapter/postgres/invite_repo_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces:
  - `usecase.PendingInvite{ID, Name, Email string; Role domain.Role; Capabilities domain.Capabilities; ExpiresAt, CreatedAt time.Time}`
  - `InviteRepository.ListPending(ctx context.Context, householdID string, now time.Time) ([]usecase.PendingInvite, error)`
  - `InviteRepository.Delete(ctx context.Context, householdID, inviteID string) error`

- [ ] **Step 1: Write the failing Postgres tests**

Append to `api/internal/adapter/postgres/invite_repo_test.go`:

```go
// createHouseholdForInviteTest keeps the two tests below about invites rather
// than about household setup.
func createHouseholdForInviteTest(t *testing.T, households *postgres.HouseholdRepo, familyName string) domain.Household {
	t.Helper()
	h, err := households.Create(context.Background(), domain.Household{
		Name: familyName + " household", FamilyName: familyName,
		PrimaryCurrency: "SGD", SecondaryCurrency: "IDR", ShowSecondaryCurrency: true,
	})
	if err != nil {
		t.Fatalf("create household %s: %v", familyName, err)
	}
	return h
}

// TestListPendingInvites pins the one definition of "pending" -- not
// accepted, not expired -- and that the list never crosses households
// (docs/LEARNING.md pattern 24). An empty result is an empty slice, not nil,
// so the HTTP layer encodes it as [].
func TestListPendingInvites(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	invites := postgres.NewInviteRepo(db)

	h := createHouseholdForInviteTest(t, households, "Oentoro")
	other := createHouseholdForInviteTest(t, households, "Someone Else")
	empty := createHouseholdForInviteTest(t, households, "Nobody Invited")
	inviter, err := users.Create(ctx, "andreas@hearth.family", "hash", "Andreas")
	if err != nil {
		t.Fatalf("create inviter: %v", err)
	}
	now := time.Now()

	liveID, err := invites.Create(ctx, h.ID, "christine@hearth.family", "Christine", domain.RoleOwner,
		domain.AllCapabilities(), []byte("pending-live-hash-pending-live-01"), inviter.ID, now.Add(72*time.Hour))
	if err != nil {
		t.Fatalf("Create (live): %v", err)
	}
	acceptedID, err := invites.Create(ctx, h.ID, "accepted@example.com", "Accepted", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar}, []byte("pending-accepted-hash-accepted-01"), inviter.ID, now.Add(72*time.Hour))
	if err != nil {
		t.Fatalf("Create (accepted): %v", err)
	}
	if err := invites.MarkAccepted(ctx, acceptedID); err != nil {
		t.Fatalf("MarkAccepted: %v", err)
	}
	if _, err := invites.Create(ctx, h.ID, "expired@example.com", "Expired", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar}, []byte("pending-expired-hash-expired-001"), inviter.ID, now.Add(-time.Hour)); err != nil {
		t.Fatalf("Create (expired): %v", err)
	}
	if _, err := invites.Create(ctx, other.ID, "stranger@example.com", "Stranger", domain.RoleOwner,
		domain.AllCapabilities(), []byte("pending-other-hash-other-house-01"), inviter.ID, now.Add(72*time.Hour)); err != nil {
		t.Fatalf("Create (other household): %v", err)
	}

	got, err := invites.ListPending(ctx, h.ID, now)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("pending = %+v, want exactly Christine's invite", got)
	}
	if got[0].ID != liveID || got[0].Email != "christine@hearth.family" || got[0].Name != "Christine" ||
		got[0].Role != domain.RoleOwner || !got[0].ExpiresAt.After(now) || got[0].CreatedAt.IsZero() {
		t.Fatalf("pending[0] = %+v", got[0])
	}

	none, err := invites.ListPending(ctx, empty.ID, now)
	if err != nil {
		t.Fatalf("ListPending (empty household): %v", err)
	}
	if none == nil || len(none) != 0 {
		t.Fatalf("empty household: got %#v, want an empty, non-nil slice", none)
	}
}

// TestDeleteInvite pins withdraw's contract: it removes an unaccepted invite
// so its token stops resolving, refuses an accepted one, and cannot reach
// another household's invite -- the id alone is never enough.
func TestDeleteInvite(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	households := postgres.NewHouseholdRepo(db)
	users := postgres.NewUserRepo(db)
	invites := postgres.NewInviteRepo(db)

	h := createHouseholdForInviteTest(t, households, "Oentoro")
	other := createHouseholdForInviteTest(t, households, "Someone Else")
	inviter, err := users.Create(ctx, "andreas@hearth.family", "hash", "Andreas")
	if err != nil {
		t.Fatalf("create inviter: %v", err)
	}
	later := time.Now().Add(72 * time.Hour)

	liveHash := []byte("delete-live-hash-delete-live-0001")
	liveID, err := invites.Create(ctx, h.ID, "christine@hearth.family", "Christine", domain.RoleOwner,
		domain.AllCapabilities(), liveHash, inviter.ID, later)
	if err != nil {
		t.Fatalf("Create (live): %v", err)
	}

	// Another household's id is not found, and its row survives.
	if err := invites.Delete(ctx, other.ID, liveID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("delete through another household: got %v, want domain.ErrNotFound", err)
	}
	if _, err := invites.ByTokenHash(ctx, liveHash); err != nil {
		t.Fatalf("the invite must survive a delete scoped to another household: %v", err)
	}

	if err := invites.Delete(ctx, h.ID, liveID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := invites.ByTokenHash(ctx, liveHash); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("after Delete the token must not resolve: got %v", err)
	}
	if err := invites.Delete(ctx, h.ID, liveID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("deleting twice: got %v, want domain.ErrNotFound", err)
	}

	acceptedHash := []byte("delete-accepted-hash-accepted-001")
	acceptedID, err := invites.Create(ctx, h.ID, "accepted@example.com", "Accepted", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar}, acceptedHash, inviter.ID, later)
	if err != nil {
		t.Fatalf("Create (accepted): %v", err)
	}
	if err := invites.MarkAccepted(ctx, acceptedID); err != nil {
		t.Fatalf("MarkAccepted: %v", err)
	}
	if err := invites.Delete(ctx, h.ID, acceptedID); !errors.Is(err, domain.ErrInviteAlreadyAccepted) {
		t.Fatalf("deleting an accepted invite: got %v, want domain.ErrInviteAlreadyAccepted", err)
	}
	if _, err := invites.ByTokenHash(ctx, acceptedHash); err != nil {
		t.Fatalf("an accepted invite is history and must survive: %v", err)
	}

	// Withdrawing an expired, unaccepted invite is tidying up, not an error.
	expiredID, err := invites.Create(ctx, h.ID, "expired@example.com", "Expired", domain.RoleLimited,
		domain.Capabilities{domain.CapCalendar}, []byte("delete-expired-hash-expired-00001"), inviter.ID, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("Create (expired): %v", err)
	}
	if err := invites.Delete(ctx, h.ID, expiredID); err != nil {
		t.Fatalf("deleting an expired invite: %v", err)
	}

	if err := invites.Delete(ctx, h.ID, "not-a-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("a malformed id: got %v, want domain.ErrNotFound", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd api && go test ./internal/adapter/postgres -run 'TestListPendingInvites|TestDeleteInvite' -v`
Expected: build failure, `invites.ListPending undefined` and `invites.Delete undefined`.

- [ ] **Step 3: Add the SQL**

In `api/internal/adapter/postgres/queries/identity.sql`, directly after the `MarkInviteAccepted` query:

```sql
-- name: ListPendingInvites :many
-- "Pending" is the partner-invite spec's one definition: not accepted and not
-- expired. $2 is the caller's clock rather than now(), so a test can move it,
-- the same shape ListPendingInvitesForAdmin uses.
SELECT id, email, name, role, capabilities, expires_at, created_at
FROM invites
WHERE household_id = $1 AND accepted_at IS NULL AND expires_at > $2
ORDER BY created_at, id;

-- name: DeleteUnacceptedInvite :one
-- Scoped by household in the SQL itself, so an id from another household
-- deletes nothing (docs/LEARNING.md pattern 24). An accepted invite is
-- history and is never deleted here.
DELETE FROM invites
WHERE id = $1 AND household_id = $2 AND accepted_at IS NULL
RETURNING id;

-- name: InviteAcceptedInHousehold :one
-- Read only after DeleteUnacceptedInvite matched nothing, to tell "already
-- accepted" apart from "no such invite in this household".
SELECT accepted_at IS NOT NULL AS accepted
FROM invites
WHERE id = $1 AND household_id = $2;
```

Run: `make sqlc`
Expected: exit 0. `api/internal/adapter/postgres/sqlcgen/identity.sql.go` now has `ListPendingInvites(ctx, ListPendingInvitesParams{HouseholdID, ExpiresAt})`, `DeleteUnacceptedInvite(ctx, DeleteUnacceptedInviteParams{ID, HouseholdID})` and `InviteAcceptedInHousehold(ctx, InviteAcceptedInHouseholdParams{ID, HouseholdID}) (bool, error)`. If sqlc chose different field names, use the generated ones; do not edit generated code.

- [ ] **Step 4: Add the type and the port methods**

In `api/internal/usecase/ports.go`, directly after the `AcceptedInvite` struct:

```go
// PendingInvite is one invite a household has sent that nobody has accepted
// and that has not expired: what Settings lists so an owner can see, and
// withdraw, what they sent. It carries no token -- the raw token is never
// stored, and the hash is nobody's business above the adapter.
type PendingInvite struct {
	ID           string
	Name         string
	Email        string
	Role         domain.Role
	Capabilities domain.Capabilities
	ExpiresAt    time.Time
	CreatedAt    time.Time
}
```

Inside `type InviteRepository interface`, after `Accept`:

```go
	// ListPending returns householdID's invites that are neither accepted
	// nor expired as of now, oldest first. "Pending" has this one definition
	// everywhere (the partner-invite spec's Data section). An empty result
	// is an empty slice, never nil, so the HTTP layer encodes it as [].
	ListPending(ctx context.Context, householdID string, now time.Time) ([]PendingInvite, error)
	// Delete removes an invite nobody has accepted, scoped to householdID in
	// the SQL itself: an id belonging to another household deletes nothing
	// and reports domain.ErrNotFound, exactly as an id that never existed
	// does, so the answer never confirms another household's invite exists.
	// An accepted invite is history rather than something to withdraw: it
	// reports domain.ErrInviteAlreadyAccepted with nothing deleted. An
	// expired, unaccepted invite is deletable.
	Delete(ctx context.Context, householdID, inviteID string) error
```

- [ ] **Step 5: Implement the Postgres methods**

Append to `api/internal/adapter/postgres/invite_repo.go`:

```go
// ListPending reads role and capabilities through toRole and toCapabilities
// like every other enum read here, so a value this build does not know fails
// the read instead of reaching the wire (CLAUDE.md: fail closed).
func (r *InviteRepo) ListPending(ctx context.Context, householdID string, now time.Time) ([]usecase.PendingInvite, error) {
	rows, err := r.q.ListPendingInvites(ctx, sqlcgen.ListPendingInvitesParams{
		HouseholdID: uuid(householdID),
		ExpiresAt:   timestamptz(now),
	})
	if err != nil {
		return nil, translate(err, "list pending invites")
	}
	out := make([]usecase.PendingInvite, 0, len(rows))
	for _, row := range rows {
		role, err := toRole(row.Role)
		if err != nil {
			return nil, err
		}
		caps, err := toCapabilities(row.Capabilities)
		if err != nil {
			return nil, err
		}
		out = append(out, usecase.PendingInvite{
			ID:           uuidToString(row.ID),
			Name:         row.Name,
			Email:        row.Email,
			Role:         role,
			Capabilities: caps,
			ExpiresAt:    timeOf(row.ExpiresAt),
			CreatedAt:    timeOf(row.CreatedAt),
		})
	}
	return out, nil
}

// Delete is one guarded statement on the common path. Only when it removed
// nothing does a second, equally household-scoped read decide which of
// InviteRepository.Delete's two refusals applies -- so neither statement can
// confirm that another household's invite id exists.
func (r *InviteRepo) Delete(ctx context.Context, householdID, inviteID string) error {
	_, err := r.q.DeleteUnacceptedInvite(ctx, sqlcgen.DeleteUnacceptedInviteParams{
		ID:          uuid(inviteID),
		HouseholdID: uuid(householdID),
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("delete invite: %w", err)
	}
	accepted, err := r.q.InviteAcceptedInHousehold(ctx, sqlcgen.InviteAcceptedInHouseholdParams{
		ID:          uuid(inviteID),
		HouseholdID: uuid(householdID),
	})
	if err != nil {
		return translate(err, "read invite acceptance")
	}
	if accepted {
		return domain.ErrInviteAlreadyAccepted
	}
	// An unaccepted row here means it changed between the two statements.
	// Answer as the DELETE did: nothing was removed.
	return domain.ErrNotFound
}
```

- [ ] **Step 6: Teach the in-memory double the same contract**

In `api/internal/usecase/testdouble_test.go`, add two fields to `inviteRow`:

```go
	CreatedAt    time.Time
	// Seq is insertion order, standing in for ListPendingInvites' ORDER BY
	// created_at, id -- a fixed test clock gives every row the same
	// CreatedAt, so the timestamp alone cannot order them.
	Seq int
```

In `inviteDouble.Create`, set both on the row it writes (`d.n` has already been incremented there):

```go
	d.rows[string(tokenHash)] = &inviteRow{
		ID: id, HouseholdID: householdID, Email: email, Name: name, Role: role,
		Capabilities: caps, InvitedBy: invitedBy, ExpiresAt: expiresAt,
		CreatedAt: d.clock.Now(), Seq: d.n,
	}
```

Append after `inviteDouble.Accept` (add `"sort"` to the file's imports if it is not already there):

```go
// ListPending mirrors ListPendingInvites: this household's rows that are
// neither accepted nor expired as of now, oldest first, never nil.
func (d *inviteDouble) ListPending(_ context.Context, householdID string, now time.Time) ([]usecase.PendingInvite, error) {
	rows := make([]*inviteRow, 0)
	for _, row := range d.rows {
		if row.HouseholdID == householdID && row.AcceptedAt == nil && row.ExpiresAt.After(now) {
			rows = append(rows, row)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Seq < rows[j].Seq })
	out := make([]usecase.PendingInvite, 0, len(rows))
	for _, row := range rows {
		out = append(out, usecase.PendingInvite{
			ID: row.ID, Name: row.Name, Email: row.Email, Role: row.Role,
			Capabilities: row.Capabilities, ExpiresAt: row.ExpiresAt, CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

// Delete mirrors DeleteUnacceptedInvite plus InviteAcceptedInHousehold: a
// row in another household is not found, an accepted row is refused and
// kept, anything else is removed.
func (d *inviteDouble) Delete(_ context.Context, householdID, inviteID string) error {
	for hash, row := range d.rows {
		if row.ID != inviteID || row.HouseholdID != householdID {
			continue
		}
		if row.AcceptedAt != nil {
			return domain.ErrInviteAlreadyAccepted
		}
		delete(d.rows, hash)
		return nil
	}
	return domain.ErrNotFound
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd api && go build ./... && go vet ./... && go test ./internal/adapter/postgres -run 'TestListPendingInvites|TestDeleteInvite|TestInviteLifecycle|TestLiveInviteForEmail' -v`
Expected: PASS. If `go build` names another type that no longer satisfies `InviteRepository`, give it the same two methods with the same contract.

- [ ] **Step 8: Commit**

```bash
git add api/internal/adapter/postgres/queries/identity.sql api/internal/adapter/postgres/sqlcgen api/internal/usecase/ports.go api/internal/adapter/postgres/invite_repo.go api/internal/adapter/postgres/invite_repo_test.go api/internal/usecase/testdouble_test.go
git commit -m "feat(invites): list pending invites and delete one, scoped by household

Claude-Session: https://claude.ai/code/session_01EVA25nn5hrgVDeR3C1m8zA"
```

---

### Task 2: Service: `InviteService.ListPending` and `InviteService.Withdraw`

**Files:**
- Modify: `api/internal/usecase/invite.go` (append after `Accept`)
- Test: `api/internal/usecase/invite_test.go` (append)

**Interfaces:**
- Consumes: `InviteRepository.ListPending`, `InviteRepository.Delete`, `usecase.PendingInvite` (Task 1).
- Produces:
  - `(*InviteService).ListPending(ctx context.Context, householdID string) ([]PendingInvite, error)`
  - `(*InviteService).Withdraw(ctx context.Context, householdID, inviteID string) error`

- [ ] **Step 1: Write the failing tests**

Append to `api/internal/usecase/invite_test.go`:

```go
// TestListPendingShowsOnlyThisHouseholdsLiveInvites pins "pending" -- not
// accepted, not expired, this household only -- at the service boundary,
// measured by the Clock the service reads rather than by wall time.
func TestListPendingShowsOnlyThisHouseholdsLiveInvites(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ownerCaps := domain.AllCapabilities()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Old", "old@example.com",
		domain.RoleOwner, ownerCaps); err != nil {
		t.Fatalf("Create (old): %v", err)
	}
	f.clock.Advance(8 * 24 * time.Hour) // past the seven-day invite TTL

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Jane", "jane@example.com",
		domain.RoleOwner, ownerCaps); err != nil {
		t.Fatalf("Create (jane): %v", err)
	}
	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Accepted", "accepted@example.com",
		domain.RoleLimited, domain.Capabilities{domain.CapCalendar}); err != nil {
		t.Fatalf("Create (accepted): %v", err)
	}
	if _, err := f.invites.Accept(ctx, lastInviteToken(t, f), "a long enough password", "Accepted"); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if _, err := f.inviteRepo.Create(ctx, "household-2", "stranger@example.com", "Stranger",
		domain.RoleOwner, ownerCaps, []byte("another-household-token-hash"), f.andreasID,
		f.clock.Now().Add(time.Hour)); err != nil {
		t.Fatalf("Create (other household): %v", err)
	}

	got, err := f.invites.ListPending(ctx, f.householdID)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(got) != 1 || got[0].Email != "jane@example.com" || got[0].Role != domain.RoleOwner {
		t.Fatalf("pending = %+v, want only Jane's invite", got)
	}
}

func TestListPendingIsEmptyNotNilForAHouseholdWithNoInvites(t *testing.T) {
	f := newFixture(t)

	got, err := f.invites.ListPending(context.Background(), f.householdID)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("got %#v, want an empty, non-nil slice", got)
	}
}

// TestWithdrawKillsTheInviteLink is the point of withdrawing: the link the
// invitee already holds must stop working, not merely vanish from a list.
func TestWithdrawKillsTheInviteLink(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Jane", "jane@example.com",
		domain.RoleOwner, domain.AllCapabilities()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	token := lastInviteToken(t, f)
	pending, err := f.invites.ListPending(ctx, f.householdID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("ListPending: %v %+v", err, pending)
	}

	if err := f.invites.Withdraw(ctx, f.householdID, pending[0].ID); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	if _, err := f.invites.Preview(ctx, token); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Preview after withdraw: got %v, want domain.ErrNotFound", err)
	}
	if left, _ := f.invites.ListPending(ctx, f.householdID); len(left) != 0 {
		t.Fatalf("still pending after withdraw: %+v", left)
	}
}

func TestWithdrawRefusesAnAcceptedInvite(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	if err := f.invites.Create(ctx, f.householdID, f.andreasID, "Jane", "jane@example.com",
		domain.RoleOwner, domain.AllCapabilities()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	pending, _ := f.invites.ListPending(ctx, f.householdID)
	if _, err := f.invites.Accept(ctx, lastInviteToken(t, f), "a long enough password", "Jane"); err != nil {
		t.Fatalf("Accept: %v", err)
	}

	if err := f.invites.Withdraw(ctx, f.householdID, pending[0].ID); !errors.Is(err, domain.ErrInviteAlreadyAccepted) {
		t.Fatalf("Withdraw after accept: got %v, want domain.ErrInviteAlreadyAccepted", err)
	}
}

func TestWithdrawCannotReachAnotherHouseholdsInvite(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	otherID, err := f.inviteRepo.Create(ctx, "household-2", "stranger@example.com", "Stranger",
		domain.RoleOwner, domain.AllCapabilities(), []byte("another-household-token-hash"), f.andreasID,
		f.clock.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("Create (other household): %v", err)
	}

	if err := f.invites.Withdraw(ctx, f.householdID, otherID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Withdraw across households: got %v, want domain.ErrNotFound", err)
	}
	if left, _ := f.inviteRepo.ListPending(ctx, "household-2", f.clock.Now()); len(left) != 1 {
		t.Fatalf("the other household's invite must survive: %+v", left)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd api && go test ./internal/usecase -run 'TestListPending|TestWithdraw' -v`
Expected: build failure, `f.invites.ListPending undefined` and `f.invites.Withdraw undefined`.

- [ ] **Step 3: Implement the two methods**

Append to `api/internal/usecase/invite.go`:

```go
// ListPending is what an owner sees in Settings: every invite the household
// has sent that nobody has accepted and that has not expired, measured
// against Clock so tests can move it. See InviteRepository.ListPending for
// the one definition of "pending".
func (s *InviteService) ListPending(ctx context.Context, householdID string) ([]PendingInvite, error) {
	return s.d.Invites.ListPending(ctx, householdID, s.d.Clock.Now())
}

// Withdraw deletes an invite nobody has accepted, so the link its invitee
// already holds stops working. It deletes rather than stamps
// (partner-invite spec decision 13): a "withdrawn" stamp would need every
// invite query to remember one more condition, and a deleted row cannot be
// accepted by any of them, current or future.
func (s *InviteService) Withdraw(ctx context.Context, householdID, inviteID string) error {
	return s.d.Invites.Delete(ctx, householdID, inviteID)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd api && go test ./internal/usecase -v -run 'TestListPending|TestWithdraw|TestCreate|TestPreview|TestAccept'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/internal/usecase/invite.go api/internal/usecase/invite_test.go
git commit -m "feat(invites): InviteService.ListPending and Withdraw

Claude-Session: https://claude.ai/code/session_01EVA25nn5hrgVDeR3C1m8zA"
```

---

### Task 3: HTTP: `GET /household/invites` and `DELETE /household/invites/{id}`

**Files:**
- Create: `api/internal/adapter/http/pending_invite_handlers.go`
- Modify: `api/internal/adapter/http/router.go` (session group ~line 247; owner-and-CSRF group ~line 265)
- Modify: `api/cmd/hearthctl/routes.go` (after the `/household/members/{id}` lines, ~line 45)
- Test: `api/internal/adapter/http/pending_invites_api_test.go` (create)

**Interfaces:**
- Consumes: `(*usecase.InviteService).ListPending`, `(*usecase.InviteService).Withdraw` (Task 2), reached through the existing `Deps.Invites`.
- Produces (the wire contract Tasks 4 and 5 read):
  - `GET /api/v1/household/invites` → `200` `[{"id","name","email","role","capabilities":[...],"expiresAt"}]` (RFC 3339 time; `[]` when none).
  - `DELETE /api/v1/household/invites/{id}` → `204`, no body. `404 NOT_FOUND` for an unknown or foreign id; `409 INVITE_ALREADY_ACCEPTED` for an accepted one; `403 SESSION_REQUIRED` for an API token.

- [ ] **Step 1: Write the failing HTTP tests**

Create `api/internal/adapter/http/pending_invites_api_test.go`:

```go
package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Pending invites: docs/superpowers/specs/2026-09-19-hearth-partner-invite-lobby-design.md,
// milestone 1.

type pendingInviteBody struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	Capabilities []string  `json:"capabilities"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

func (env *testEnv) mustInviteOwner(t *testing.T, session, csrf *http.Cookie, name, email string) {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/household/members/invite", map[string]any{
		"name": name, "email": email, "role": "owner",
		"capabilities": []string{"calendar", "chores", "money", "marriage"},
	}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("invite %s: %d %s", email, rec.Code, rec.Body.String())
	}
}

func (env *testEnv) pendingInvites(t *testing.T, session *http.Cookie) []pendingInviteBody {
	t.Helper()
	rec := env.authedGet(t, "/api/v1/household/invites", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("list pending invites: %d %s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) == "null" {
		t.Fatal("an empty pending list must be [], never null")
	}
	var body []pendingInviteBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode pending invites: %v %s", err, rec.Body.String())
	}
	return body
}

func findPendingInvite(invites []pendingInviteBody, email string) (pendingInviteBody, bool) {
	for _, invite := range invites {
		if invite.Email == email {
			return invite, true
		}
	}
	return pendingInviteBody{}, false
}

func TestAnOwnerSeesAPendingInviteAndWithdrawsIt(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	env.mustInviteOwner(t, session, csrf, "Jane", "jane@example.com")

	invite, ok := findPendingInvite(env.pendingInvites(t, session), "jane@example.com")
	if !ok {
		t.Fatal("the invite just sent is not in the pending list")
	}
	if invite.Name != "Jane" || invite.Role != "owner" || !invite.ExpiresAt.After(time.Now()) {
		t.Fatalf("pending invite = %+v", invite)
	}

	rec := env.authed(t, http.MethodDelete, "/api/v1/household/invites/"+invite.ID, nil, session, csrf)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("withdraw: %d %q", rec.Code, rec.Body.String())
	}
	if _, ok := findPendingInvite(env.pendingInvites(t, session), "jane@example.com"); ok {
		t.Fatal("a withdrawn invite is still listed")
	}
	// A second withdraw is a 404: the row is gone, not merely stamped.
	if rec := env.authed(t, http.MethodDelete, "/api/v1/household/invites/"+invite.ID, nil, session, csrf); rec.Code != http.StatusNotFound {
		t.Fatalf("second withdraw: %d %s", rec.Code, rec.Body.String())
	}
}

// An invitee's address is personal data, so the list is owner-only -- the
// same rule that lets only an owner see members' addresses.
func TestThePendingInviteListIsOwnerOnly(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.limitedEmail, env.limitedPassword)

	if rec := env.authedGet(t, "/api/v1/household/invites", session); rec.Code != http.StatusForbidden {
		t.Fatalf("a limited member listing invites: %d %s", rec.Code, rec.Body.String())
	}
}

// Reading shows no secret, so a token may list. Withdrawing changes who may
// join, so a token may not (spec decision 12, the reason behind ADR 7 rule 2).
func TestATokenCanListPendingInvitesButNotWithdrawThem(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	env.mustInviteOwner(t, session, csrf, "Jane", "jane@example.com")
	tok := env.mustCreateToken(t, session, csrf, "agent")

	if rec := env.bearer(t, http.MethodGet, "/api/v1/household/invites", nil, tok.Token); rec.Code != http.StatusOK {
		t.Fatalf("a token listing invites: %d %s", rec.Code, rec.Body.String())
	}

	invite, ok := findPendingInvite(env.pendingInvites(t, session), "jane@example.com")
	if !ok {
		t.Fatal("setup: the invite is not pending")
	}
	rec := env.bearer(t, http.MethodDelete, "/api/v1/household/invites/"+invite.ID, nil, tok.Token)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "SESSION_REQUIRED") {
		t.Fatalf("a token withdrawing an invite: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := findPendingInvite(env.pendingInvites(t, session), "jane@example.com"); !ok {
		t.Fatal("a refused withdraw still deleted the invite")
	}
}

func TestWithdrawingAnUnknownInviteIs404(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	for _, id := range []string{"00000000-0000-0000-0000-000000000000", "not-a-uuid"} {
		if rec := env.authed(t, http.MethodDelete, "/api/v1/household/invites/"+id, nil, session, csrf); rec.Code != http.StatusNotFound {
			t.Fatalf("withdraw %q: %d %s", id, rec.Code, rec.Body.String())
		}
	}
}
```

`TestOwnerOnlyRoutesRejectALimitedMember` (`household_api_test.go`) walks every mutating route, so it covers the new `DELETE` for a limited member with no edit.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd api && go test ./internal/adapter/http -run 'PendingInvite|WithdrawingAnUnknownInvite' -v`
Expected: FAIL, the list answers `404` (no route yet).

- [ ] **Step 3: Write the handlers**

Create `api/internal/adapter/http/pending_invite_handlers.go`:

```go
package httpadapter

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// An owner's view of the invites their household has sent and nobody has
// accepted yet (partner-invite spec, milestone 1). Before this an invite was
// written and never read back: the modal closed and Settings showed no trace,
// so an owner could not tell a sent invite from a lost one. The public,
// pre-sign-in invite routes live in invite_handlers.go.

// pendingInviteDTO is one row of GET /household/invites. Email is always the
// real address: the route is owner-only, the same rule that lets only an
// owner see members' addresses (handleListMembers).
type pendingInviteDTO struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	Capabilities []string  `json:"capabilities"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

func handleListPendingInvites(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		invites, err := deps.Invites.ListPending(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		out := make([]pendingInviteDTO, 0, len(invites))
		for _, invite := range invites {
			out = append(out, pendingInviteDTO{
				ID:           invite.ID,
				Name:         invite.Name,
				Email:        invite.Email,
				Role:         string(invite.Role),
				Capabilities: invite.Capabilities.Strings(),
				ExpiresAt:    invite.ExpiresAt,
			})
		}
		WriteJSON(w, http.StatusOK, out)
	}
}

// handleWithdrawInvite answers 204: the row is gone, so there is nothing to
// describe. Its guards live on the route (router.go): owner, CSRF, and a
// browser session.
func handleWithdrawInvite(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, ok := requireScope(w, r)
		if !ok {
			return
		}
		if err := deps.Invites.Withdraw(r.Context(), scope.HouseholdID, chi.URLParam(r, "id")); err != nil {
			MapDomainError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
```

- [ ] **Step 4: Register the routes**

In `api/internal/adapter/http/router.go`, inside the `api.Group(func(g chi.Router) {` block, directly after `g.Get("/notification-preferences", handleGetNotificationPreferences(deps))`:

```go
			// Owner-only: an invitee's address is personal data. Readable with
			// an API token -- it shows no secret -- unlike withdrawing, below.
			g.Group(func(o chi.Router) {
				o.Use(requireOwner)
				o.Get("/household/invites", handleListPendingInvites(deps))
			})
```

In the owner group inside `m.Use(requireCSRF)` (the one ending `o.Post("/spaces", handleCreateSpace(deps))`), add after that line:

```go
						// Withdrawing an invite changes who may join the
						// household, so it needs a browser session as well as
						// an owner: a leaked API token must not be able to
						// manage who gets in (partner-invite spec decision 12,
						// the reason behind ADR 7 rule 2).
						o.Group(func(c chi.Router) {
							c.Use(requireCookieSession)
							c.Delete("/household/invites/{id}", handleWithdrawInvite(deps))
						})
```

- [ ] **Step 5: Add both routes to hearthctl's manual**

In `api/cmd/hearthctl/routes.go`, after `{"DELETE", "/household/members/{id}", "owner+csrf", "-"},`:

```go
	{"GET", "/household/invites", "owner", "-"},
	{"DELETE", "/household/invites/{id}", "owner+browser session+csrf", "-"},
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd api && go test ./internal/adapter/http -run 'PendingInvite|WithdrawingAnUnknownInvite|TestOwnerOnlyRoutesRejectALimitedMember' -v && go test ./cmd/hearthctl -v`
Expected: PASS, including `TestRouteTableMatchesRouterGo`.

- [ ] **Step 7: Commit**

```bash
git add api/internal/adapter/http/pending_invite_handlers.go api/internal/adapter/http/pending_invites_api_test.go api/internal/adapter/http/router.go api/cmd/hearthctl/routes.go
git commit -m "feat(invites): GET /household/invites and DELETE /household/invites/{id}

Withdrawing needs a browser session: a leaked token must not manage who
may join the household.

Claude-Session: https://claude.ai/code/session_01EVA25nn5hrgVDeR3C1m8zA"
```

---

### Task 4: Frontend: the pending invites list in Settings

**Files:**
- Modify: `web/src/features/settings/schemas.ts` (after `membersListSchema`)
- Create: `web/src/features/settings/usePendingInvites.ts`
- Modify: `web/src/features/settings/useInviteMember.ts`
- Modify: `web/src/features/settings/copy.ts`
- Create: `web/src/features/settings/PendingInvitesList.tsx`
- Modify: `web/src/features/settings/MembersPanel.tsx`
- Test: `web/src/features/settings/PendingInvitesList.test.tsx` (create), `web/src/features/settings/MembersPanel.test.tsx`

**Interfaces:**
- Consumes: the wire contract from Task 3.
- Produces:
  - `pendingInviteSchema`, `pendingInvitesSchema`, `type PendingInvite = { id; name; email; role; capabilities: string[]; expiresAt: string }` (from `schemas.ts`)
  - `pendingInvitesQueryKey = ["household", "invites"] as const`
  - `usePendingInvites({ enabled }: { enabled: boolean })`, `useWithdrawInvite()` (from `usePendingInvites.ts`)
  - `pendingInviteExpiryLine(expiresAt: string): string` (from `copy.ts`)
  - `PendingInvitesList` component (no props)

- [ ] **Step 1: Write the failing component test**

Create `web/src/features/settings/PendingInvitesList.test.tsx`:

```tsx
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { PendingInvitesList } from "./PendingInvitesList";
import type { PendingInvite } from "./schemas";

const INVITES_URL = "/api/v1/household/invites";

const jane: PendingInvite = {
  id: "inv-jane",
  name: "Jane",
  email: "jane@example.com",
  role: "owner",
  capabilities: ["calendar", "chores", "money", "marriage"],
  expiresAt: "2026-09-26T09:00:00Z",
};

function renderList() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <PendingInvitesList />
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("PendingInvitesList", () => {
  it("lists each pending invite with its role, address and expiry", async () => {
    stubFetchRoutes({ [`GET ${INVITES_URL}`]: { status: 200, body: [jane] } });
    renderList();

    expect(await screen.findByText("Jane")).toBeInTheDocument();
    expect(screen.getByText(/^Owner · jane@example\.com · Expires /)).toBeInTheDocument();
  });

  it("renders nothing when no invite is pending", async () => {
    const fetchMock = stubFetchRoutes({ [`GET ${INVITES_URL}`]: { status: 200, body: [] } });
    const { container } = renderList();

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });

  it("withdraws an invite and drops it from the list", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${INVITES_URL}`]: [
        { status: 200, body: [jane] },
        { status: 200, body: [] },
      ],
      [`DELETE ${INVITES_URL}/inv-jane`]: { status: 204, body: undefined },
    });
    renderList();

    fireEvent.click(await screen.findByRole("button", { name: "Withdraw the invite to Jane" }));

    await waitFor(() => expect(screen.queryByText("Jane")).toBeNull());
    expect(
      fetchMock.mock.calls.some(
        ([input, init]) => String(input) === `${INVITES_URL}/inv-jane` && init?.method === "DELETE",
      ),
    ).toBe(true);
  });

  it("disables Withdraw while its request runs, so a double click sends one DELETE", async () => {
    let release: () => void = () => {};
    const routed = stubFetchRoutes({
      [`GET ${INVITES_URL}`]: { status: 200, body: [jane] },
      [`DELETE ${INVITES_URL}/inv-jane`]: { status: 204, body: undefined },
    });
    const gated = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "DELETE") await new Promise<void>((r) => (release = r));
      return routed(input, init);
    });
    vi.stubGlobal("fetch", gated);
    renderList();

    const button = await screen.findByRole("button", { name: "Withdraw the invite to Jane" });
    fireEvent.click(button);
    fireEvent.click(button);
    await waitFor(() => expect(button).toBeDisabled());
    release();

    expect(gated.mock.calls.filter(([, init]) => init?.method === "DELETE")).toHaveLength(1);
  });

  it("shows the server's message when a withdraw fails", async () => {
    stubFetchRoutes({
      [`GET ${INVITES_URL}`]: { status: 200, body: [jane] },
      [`DELETE ${INVITES_URL}/inv-jane`]: {
        status: 409,
        body: { error: { code: "INVITE_ALREADY_ACCEPTED", message: "This invite has already been accepted." } },
      },
    });
    renderList();

    fireEvent.click(await screen.findByRole("button", { name: "Withdraw the invite to Jane" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("This invite has already been accepted.");
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/features/settings/PendingInvitesList.test.tsx`
Expected: FAIL, `Failed to resolve import "./PendingInvitesList"`.

- [ ] **Step 3: Add the schema**

In `web/src/features/settings/schemas.ts`, after `export const membersListSchema = z.array(memberSchema);`:

```ts
// GET /household/invites' one row (pending_invite_handlers.go's
// pendingInviteDTO). Owner-only, so `email` is always the real address.
// Milestone 2 of the partner-invite spec adds `channel` and `knock`; until
// then every row is an email invite.
export const pendingInviteSchema = z.object({
  id: z.string(),
  name: z.string(),
  email: z.string(),
  role: z.string(),
  capabilities: z.array(z.string()),
  expiresAt: z.string(),
});
export type PendingInvite = z.infer<typeof pendingInviteSchema>;

export const pendingInvitesSchema = z.array(pendingInviteSchema);
```

- [ ] **Step 4: Add the hooks**

Create `web/src/features/settings/usePendingInvites.ts`:

```ts
// The household's pending invites -- sent, not accepted, not expired -- read
// by Settings' Members panel and Overview's setup checklist. The route is
// owner-only, so callers pass `enabled: isOwner` and a limited member never
// fires a request whose 403 would then need hiding.
//
// The key starts with "household" for the reason householdMembersQueryKey's
// does (useHouseholdMembers.ts): PATCH /household invalidates by that prefix.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch, fetchAndParse } from "../../api/client";
import { pendingInvitesSchema, type PendingInvite } from "./schemas";

export const pendingInvitesQueryKey = ["household", "invites"] as const;

async function fetchPendingInvites(): Promise<PendingInvite[]> {
  return fetchAndParse(pendingInvitesSchema, "/api/v1/household/invites");
}

export function usePendingInvites({ enabled }: { enabled: boolean }) {
  return useQuery({ queryKey: pendingInvitesQueryKey, queryFn: fetchPendingInvites, enabled });
}

export function useWithdrawInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    // 204, so nothing to parse: the mutation's only job is the DELETE.
    mutationFn: async (id: string) => {
      await apiFetch<unknown>(`/api/v1/household/invites/${encodeURIComponent(id)}`, {
        method: "DELETE",
      });
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: pendingInvitesQueryKey }),
  });
}
```

In `web/src/features/settings/useInviteMember.ts`, import the key and refresh it on success, so a new invite appears in the list at once:

```ts
import { pendingInvitesQueryKey } from "./usePendingInvites";
```

```ts
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: householdMembersQueryKey });
      queryClient.invalidateQueries({ queryKey: pendingInvitesQueryKey });
      queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
```

- [ ] **Step 5: Add the expiry copy**

Append to `web/src/features/settings/copy.ts`:

```ts
// "Expires 26 Sep" -- the day only: an invite lives seven days and nobody
// needs the minute. Formatted in the viewer's locale and zone.
export function pendingInviteExpiryLine(expiresAt: string): string {
  const day = new Date(expiresAt).toLocaleDateString(undefined, { day: "numeric", month: "short" });
  return `Expires ${day}`;
}
```

- [ ] **Step 6: Write the component**

Create `web/src/features/settings/PendingInvitesList.tsx`:

```tsx
// The pending half of the Members panel: every invite this household has
// sent that nobody has accepted and that has not expired, each with
// Withdraw (partner-invite spec, milestone 1). Before this an invite was
// written and never read back, so an owner could not tell a sent invite from
// a lost one.
//
// Owner-only, like the route behind it. MembersPanel mounts it only for an
// owner, so a limited member never fires the request at all.
import { useState } from "react";
import { apiErrorMessage } from "../../api/errorMessage";
import { memberBadgeLabel, pendingInviteExpiryLine } from "./copy";
import { type PendingInvite } from "./schemas";
import { usePendingInvites, useWithdrawInvite } from "./usePendingInvites";

function PendingInviteRow({
  invite,
  withdrawing,
  errorMessage,
  onWithdraw,
}: {
  invite: PendingInvite;
  withdrawing: boolean;
  errorMessage?: string;
  onWithdraw: () => void;
}) {
  return (
    <li className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="text-[13.5px] font-semibold text-ink">{invite.name}</div>
          <div className="truncate text-[11.5px] text-muted">
            {memberBadgeLabel(invite.role)} · {invite.email} · {pendingInviteExpiryLine(invite.expiresAt)}
          </div>
        </div>
        <button
          type="button"
          onClick={onWithdraw}
          disabled={withdrawing}
          aria-label={`Withdraw the invite to ${invite.name}`}
          // min-h-11/sm:min-h-0: MembersPanel's "+ Invite" comment has the
          // reason -- an unpadded text button misses the 44px phone floor.
          className="min-h-11 flex-none text-xs font-semibold text-danger disabled:opacity-50 sm:min-h-0"
        >
          Withdraw
        </button>
      </div>
      {errorMessage && (
        <p role="alert" className="text-[11px] text-danger">
          {errorMessage}
        </p>
      )}
    </li>
  );
}

export function PendingInvitesList() {
  const invites = usePendingInvites({ enabled: true });
  const withdraw = useWithdrawInvite();
  // A Set, not one flag, for the reason MembersPanel's pendingIds gives: one
  // shared mutation's isPending only reflects the latest call. It is also
  // what stops a double click sending two DELETEs.
  const [withdrawingIds, setWithdrawingIds] = useState<Set<string>>(new Set());
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});

  if (invites.isError) {
    return (
      <p role="alert" className="mt-4 text-xs text-danger">
        Couldn't load pending invites.
      </p>
    );
  }
  // Loading and "none pending" both render nothing: a heading over an empty
  // list would be noise on every settled household's Settings page.
  if (!invites.isSuccess || invites.data.length === 0) return null;

  function handleWithdraw(id: string) {
    // Checked directly, not through the disabled attribute: a second,
    // synchronous click lands before React has re-rendered the button as
    // disabled.
    if (withdrawingIds.has(id)) return;
    setRowErrors((prev) => ({ ...prev, [id]: "" }));
    setWithdrawingIds((prev) => new Set(prev).add(id));
    withdraw.mutate(id, {
      onError: (error) => {
        setRowErrors((prev) => ({
          ...prev,
          [id]: apiErrorMessage(error, "Couldn't withdraw that invite. Please try again."),
        }));
      },
      onSettled: () => {
        setWithdrawingIds((prev) => {
          const next = new Set(prev);
          next.delete(id);
          return next;
        });
      },
    });
  }

  return (
    <div className="mt-5 border-t border-hairline pt-4">
      <h3 className="text-xs font-semibold text-label">Pending invites</h3>
      <ul className="mt-2.5 flex flex-col gap-3">
        {invites.data.map((invite) => (
          <PendingInviteRow
            key={invite.id}
            invite={invite}
            withdrawing={withdrawingIds.has(invite.id)}
            errorMessage={rowErrors[invite.id]}
            onWithdraw={() => handleWithdraw(invite.id)}
          />
        ))}
      </ul>
    </div>
  );
}
```

The `withdrawingIds.has(id)` check only helps once a render has happened between the two clicks. If the double-click test in Step 1 still sees two `DELETE`s, keep the guard in a `useRef<Set<string>>` alongside the state (the ref is updated synchronously), and read the ref in the early return instead.

- [ ] **Step 7: Mount it in the Members panel**

In `web/src/features/settings/MembersPanel.tsx`, import it:

```tsx
import { PendingInvitesList } from "./PendingInvitesList";
```

and render it directly after the `{members.isSuccess && ( ... )}` block, before `<InviteMemberModal ...>`:

```tsx
      {isOwner && <PendingInvitesList />}
```

- [ ] **Step 8: Register the new route in the Members panel tests, and pin the limited case**

In `web/src/features/settings/MembersPanel.test.tsx`, add beside `MEMBERS_URL`:

```tsx
const INVITES_URL = "/api/v1/household/invites";
```

Add `[`GET ${INVITES_URL}`]: { status: 200, body: [] },` to the `stubFetchRoutes({...})` of **every test that uses `meFixture("owner")`**. Then add:

```tsx
  it("never asks a limited member's browser for pending invites", async () => {
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture("limited") },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas, kayla, ethan] },
    });
    renderPanel();

    await screen.findByText("Parent · full access");
    expect(fetchMock.mock.calls.some(([input]) => String(input) === INVITES_URL)).toBe(false);
    expect(screen.queryByText("Pending invites")).toBeNull();
  });
```

- [ ] **Step 9: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/settings`
Expected: PASS.

Then run the whole frontend suite: `cd web && npx vitest run`. Any test that now fails with `stubFetchRoutes: no stub registered for "GET /api/v1/household/invites"` renders the Members panel as an owner. Add `"GET /api/v1/household/invites": { status: 200, body: [] }` to that test's routes, and nothing else. Expected afterwards: PASS.

- [ ] **Step 10: Commit**

```bash
git add web/src/features/settings
git commit -m "feat(settings): list pending invites with Withdraw in the Members panel

Claude-Session: https://claude.ai/code/session_01EVA25nn5hrgVDeR3C1m8zA"
```

If Step 9 touched test files outside `web/src/features/settings`, add them to the same commit.

---

### Task 5: Overview: the "Invite your partner" checklist step

**Files:**
- Create: `web/src/features/overview/partnerStep.ts`
- Test: `web/src/features/overview/partnerStep.test.ts` (create)
- Modify: `web/src/features/overview/copy.ts` (after `setupBudget`, ~line 92)
- Modify: `web/src/features/overview/SetupChecklist.tsx`
- Modify: `web/src/features/overview/OverviewPage.tsx`
- Test: `web/src/features/overview/OverviewPage.test.tsx`

**Interfaces:**
- Consumes: `usePendingInvites`, `type PendingInvite` (Task 4); `useHouseholdMembers`, `type MemberView` (existing).
- Produces: `type PartnerStep = "none" | "invited" | "joined"`, `partnerStep(members: MemberView[], invites: PendingInvite[]): PartnerStep`, and a `partner: PartnerStep` prop on `SetupChecklist`.

- [ ] **Step 1: Write the failing unit test**

Create `web/src/features/overview/partnerStep.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import type { MemberView, PendingInvite } from "../settings/schemas";
import { partnerStep } from "./partnerStep";

function member(role: string, id: string): MemberView {
  return {
    id,
    user: { id: `u-${id}`, email: "", displayName: id, avatarInitial: id[0].toUpperCase() },
    role,
    capabilities: [],
  };
}

function invite(role: string): PendingInvite {
  return {
    id: `inv-${role}`,
    name: "Jane",
    email: "jane@example.com",
    role,
    capabilities: [],
    expiresAt: "2026-09-26T09:00:00Z",
  };
}

describe("partnerStep", () => {
  it("is joined once a second owner exists", () => {
    expect(partnerStep([member("owner", "a"), member("owner", "c")], [])).toBe("joined");
  });

  it("is invited while an owner-role invite is pending", () => {
    expect(partnerStep([member("owner", "a")], [invite("owner")])).toBe("invited");
  });

  // Kids do not unlock Agreements, so neither a limited member nor a pending
  // limited invite moves this step.
  it("ignores limited members and limited invites", () => {
    expect(partnerStep([member("owner", "a"), member("limited", "k")], [invite("limited")])).toBe("none");
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/features/overview/partnerStep.test.ts`
Expected: FAIL, `Failed to resolve import "./partnerStep"`.

- [ ] **Step 3: Write `partnerStep.ts`**

Create `web/src/features/overview/partnerStep.ts`:

```ts
// Where a household is on "Invite your partner". Only owners count: the step
// exists to get a household to the two owners Agreements needs to unlock, not
// to count kids. A plain .ts module rather than inside SetupChecklist.tsx, so
// react-refresh's only-export-components rule has nothing to object to.
import type { MemberView, PendingInvite } from "../settings/schemas";

export type PartnerStep = "none" | "invited" | "joined";

export function partnerStep(members: MemberView[], invites: PendingInvite[]): PartnerStep {
  if (members.filter((m) => m.role === "owner").length >= 2) return "joined";
  if (invites.some((i) => i.role === "owner")) return "invited";
  return "none";
}
```

Run: `cd web && npx vitest run src/features/overview/partnerStep.test.ts`
Expected: PASS.

- [ ] **Step 4: Change the Overview tests to the new behaviour (they must fail first)**

In `web/src/features/overview/OverviewPage.test.tsx`:

1. Next to `ACCOUNT`, add a two-owner roster:

```tsx
// Two owners: the state in which "Invite your partner" is done. Tests about a
// finished household register this; the default roster ([]) is a household
// still waiting for its partner.
const TWO_OWNERS = [
  {
    id: "m1",
    user: { id: "u1", email: "sam@newhouse.test", displayName: "Sam", avatarInitial: "S" },
    role: "owner",
    capabilities: ["calendar", "chores", "money", "marriage"],
  },
  {
    id: "m2",
    user: { id: "u2", email: "alex@newhouse.test", displayName: "Alex", avatarInitial: "A" },
    role: "owner",
    capabilities: ["calendar", "chores", "money", "marriage"],
  },
];
```

2. In `renderOverview`'s defaults, after the `"GET /api/v1/household/members"` line, add:

```tsx
    "GET /api/v1/household/invites": { status: 200, body: [] },
```

3. In `"shows a fresh household what is left to set up"`, replace the assertions after `findByText("Finish setting up")` with:

```tsx
    expect(screen.getByText("1 of 4 done")).toBeInTheDocument();

    // Each unfinished step's own link, not just the count: a checklist that
    // shows the right number of steps and sends you to the wrong screen is
    // worse than no checklist. The account step is the one the walk reached
    // through "+ Add" instead, so nothing else covers it.
    const [accountStep, budgetStep, partnerStepLink] = screen.getAllByRole("link", { name: "Set up" });
    expect(accountStep).toHaveAttribute("href", "/money");
    expect(budgetStep).toHaveAttribute("href", "/money/budget");
    expect(partnerStepLink).toHaveAttribute("href", "/settings?invite=true");
```

4. In `"drops the checklist once the household has finished setting up"`, add to its routes:

```tsx
      "GET /api/v1/household/members": { status: 200, body: TWO_OWNERS },
```

5. In `"shows no checklist before the data it reads has arrived"`, change its inline `"GET /api/v1/household/members"` entry to `{ status: 200, body: TWO_OWNERS }` and add `"GET /api/v1/household/invites": { status: 200, body: [] },` beside it.

6. In `"keeps the checklist away from a limited member, who cannot do any of it"`, change `renderOverview({` to `const { fetchMock } = renderOverview({` and add at the end:

```tsx
    expect(fetchMock.mock.calls.some(([input]) => String(input) === "/api/v1/household/invites")).toBe(false);
```

7. Add a new test after `"drops the checklist once..."`:

```tsx
  it("tells an owner whose partner is invited that the invite is on its way", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [ACCOUNT], summary: summaryBody(500000) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/goals": { status: 200, body: goalsBody() },
      "GET /api/v1/bills": { status: 200, body: billsBody() },
      "GET /api/v1/retros": { status: 200, body: retrosBody() },
      [`GET /api/v1/marriage/vision?year=${YEAR}`]: { status: 200, body: { vision: visionBody() } },
      "GET /api/v1/household/invites": {
        status: 200,
        body: [
          {
            id: "inv-1",
            name: "Alex",
            email: "alex@newhouse.test",
            role: "owner",
            capabilities: [],
            expiresAt: "2026-09-26T09:00:00Z",
          },
        ],
      },
    });

    expect(await screen.findByText(OVERVIEW_COPY.setupPartnerInvited)).toBeInTheDocument();
    expect(screen.getByText("3 of 4 done")).toBeInTheDocument();
    // Sent already, so the link shows the invite rather than opening a second one.
    expect(screen.getByRole("link", { name: OVERVIEW_COPY.setupPartnerSee })).toHaveAttribute("href", "/settings");
  });
```

Run: `cd web && npx vitest run src/features/overview/OverviewPage.test.tsx`
Expected: FAIL. `"1 of 4 done"` is not found, and `OVERVIEW_COPY.setupPartnerInvited` does not exist.

- [ ] **Step 5: Add the copy**

In `web/src/features/overview/copy.ts`, after `setupBudget`:

```ts
  // "Partner", not "member": the step exists to reach the two owners
  // Agreements needs, so a kid's invite does not tick it (partnerStep.ts).
  setupPartner: "Invite your partner",
  setupPartnerInvited: "Invite sent — waiting for your partner",
  setupPartnerSee: "See invite",
```

- [ ] **Step 6: Add the step to `SetupChecklist.tsx`**

Replace the header comment paragraph that begins `// There is deliberately no "invite your partner" step` (down to `// exposes pending invites.`) with:

```tsx
// "Invite your partner" reads the roster and the pending invites
// (partnerStep.ts). It could not exist until GET /household/invites did: an
// invite was never read back, so the step could only tick when the partner
// *accepted*, leaving an owner who had just invited someone looking at an
// unticked step whose link showed no trace of the invite they sent.
```

Replace everything from `import { Link }` to the end of the file with the version below. Each step now carries its own link element, because the partner step's link differs by state, and one union-typed `to` cannot express "with a search param" and "without" at once:

```tsx
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { OVERVIEW_COPY } from "./copy";
import type { PartnerStep } from "./partnerStep";

// Read at render time -- a household that opens the app in August must not be
// told to budget for July.
function monthName(): string {
  return new Date().toLocaleString(undefined, { month: "long" });
}

// inline-flex items-center min-h-11 sm:min-h-0: BudgetCard.tsx's own comment
// on this identical pattern has the reason.
const GO_LINK = "inline-flex min-h-11 items-center text-[12.5px] font-semibold text-accent sm:min-h-0";

export function SetupChecklist({
  hasAccount,
  hasBudget,
  partner,
}: {
  hasAccount: boolean;
  hasBudget: boolean;
  partner: PartnerStep;
}) {
  const steps: { label: string; done: boolean; link: ReactNode }[] = [
    // Always done: reaching this page at all required creating one. It is
    // listed anyway so the first thing a new household sees is something
    // already achieved rather than everything outstanding.
    { label: OVERVIEW_COPY.setupHousehold, done: true, link: null },
    {
      label: OVERVIEW_COPY.setupAccount,
      done: hasAccount,
      link: (
        <Link to="/money" className={GO_LINK}>
          {OVERVIEW_COPY.setupGo}
        </Link>
      ),
    },
    {
      label: OVERVIEW_COPY.setupBudget(monthName()),
      done: hasBudget,
      link: (
        <Link to="/money/budget" className={GO_LINK}>
          {OVERVIEW_COPY.setupGo}
        </Link>
      ),
    },
    {
      label: partner === "invited" ? OVERVIEW_COPY.setupPartnerInvited : OVERVIEW_COPY.setupPartner,
      // "Invited" is not done: the step finishes when the partner is in, not
      // when the invite leaves.
      done: partner === "joined",
      // Once an invite is out, the link shows it rather than opening a second
      // invite modal over it.
      link:
        partner === "invited" ? (
          <Link to="/settings" className={GO_LINK}>
            {OVERVIEW_COPY.setupPartnerSee}
          </Link>
        ) : (
          <Link to="/settings" search={{ invite: true }} className={GO_LINK}>
            {OVERVIEW_COPY.setupGo}
          </Link>
        ),
    },
  ];

  const done = steps.filter((s) => s.done).length;
  if (done === steps.length) return null;

  return (
    <section
      aria-labelledby="overview-setup-heading"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <div className="flex items-baseline justify-between">
        <h2 id="overview-setup-heading" className="text-sm font-semibold text-ink">
          {OVERVIEW_COPY.setupHeading}
        </h2>
        <span className="text-[11.5px] text-muted">
          {OVERVIEW_COPY.setupProgress(done, steps.length)}
        </span>
      </div>

      <ul className="mt-3 flex flex-col gap-2.5">
        {steps.map((step) => (
          <li key={step.label} className="flex items-center justify-between text-[13px]">
            <span className={step.done ? "text-muted line-through" : "text-ink"}>
              {step.done ? "✓ " : ""}
              {step.label}
            </span>
            {!step.done && step.link}
          </li>
        ))}
      </ul>
    </section>
  );
}
```

- [ ] **Step 7: Feed the step from `OverviewPage.tsx`**

Add the imports:

```tsx
import { useHouseholdMembers } from "../settings/useHouseholdMembers";
import { usePendingInvites } from "../settings/usePendingInvites";
import { partnerStep } from "./partnerStep";
```

After `const goals = useGoals({ enabled: isOwner });`:

```tsx
  // The checklist's partner step reads the roster and the pending invites.
  // GET /household/invites is owner-only, so a limited member never asks.
  const members = useHouseholdMembers();
  const invites = usePendingInvites({ enabled: isOwner });
```

Change the checklist mount to wait for both, for the reason its JSX comment already gives for accounts and budget (a claim derived from data that has not arrived), and add "the roster and the pending invites" to that comment's list of queries:

```tsx
          {isOwner && accounts.isSuccess && budget.data && members.isSuccess && invites.isSuccess && (
            <SetupChecklist
              hasAccount={accounts.data.accounts.length > 0}
              hasBudget={budget.data.budget != null}
              partner={partnerStep(members.data, invites.data)}
            />
          )}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/overview && npx tsc --noEmit -p .`
Expected: PASS, no type errors. If `tsc` rejects `search={{ invite: true }}`, `AgreementsPage.tsx:232` already passes the same search to the same route; match its exact form.

- [ ] **Step 9: Commit**

```bash
git add web/src/features/overview
git commit -m "feat(overview): Invite your partner joins the setup checklist

Claude-Session: https://claude.ai/code/session_01EVA25nn5hrgVDeR3C1m8zA"
```

---

### Task 6: Prove it: mutation checks, full gate, browser walk, docs

**Files:**
- Create: `docs/superpowers/plans/2026-09-19-hearth-partner-invite-lobby-m1-verification.md`
- Modify: `docs/FEATURE_TRACKER.md`, `docs/SYSTEM_DESIGN.md`, `docs/LEARNING.md`, `docs/HANDOVER.md`

**Interfaces:** consumes everything above; produces the evidence and the docs.

- [ ] **Step 1: Run the four named mutation checks**

Make each change, run the named test, confirm it goes **red**, then revert with `git checkout -- <file>`. Record each result (the failing assertion's message) in the verification file.

| # | Mutation | Test that must fail |
|---|---|---|
| 1 | In `identity.sql`'s `ListPendingInvites`, delete `household_id = $1 AND ` (then `make sqlc`) | `TestListPendingInvites` |
| 2 | In `DeleteUnacceptedInvite`, delete ` AND household_id = $2` (then `make sqlc`) | `TestDeleteInvite` ("must survive a delete scoped to another household") |
| 3 | In `router.go`, delete `c.Use(requireCookieSession)` | `TestATokenCanListPendingInvitesButNotWithdrawThem` |
| 4 | In `partnerStep.ts`, change `>= 2` to `>= 1` | `partnerStep.test.ts` ("ignores limited members…") and OverviewPage ("shows a fresh household…") |

After reverting mutations 1 and 2, run `make sqlc` again and confirm `git status` shows no change under `sqlcgen/`.

- [ ] **Step 2: Run the full gate**

Run: `make lint && make test`
Expected: both green. Paste the closing lines of each into the verification file.

- [ ] **Step 3: Walk it in a real browser**

Follow the `verifying-in-the-real-environment` skill.
- First check which Docker engine serves port 5173 (`lsof -i :5173`); two engines host this stack.
- Then `make dev`, `make seed`, and sign in as the seeded owner.
- **Restart the web container after the last frontend edit** (`docker restart hearth-web-1`); the 2026-09-13 walk ran against stale modules.

Drive http://localhost:5173 with Playwright or Chrome DevTools, using real clicks and keys. Record pass or fail and the evidence per criterion in the verification file:

1. Settings → "+ Invite", invite "Jane", `jane@example.com`, Owner. The modal closes and a **Pending invites** row for Jane appears without a reload.
2. The row reads `Owner · jane@example.com · Expires <a date seven days ahead>`.
3. Mailpit (http://localhost:8025) holds Jane's invite email, and its link opens the invite preview screen.
4. Back in Settings, click **Withdraw** on Jane. The row disappears; the network log shows one `DELETE /api/v1/household/invites/<id>` answered `204`.
5. Reopen Jane's link from Mailpit: it now shows the not-found or expired invite screen, not a preview.
6. Double-click **Withdraw** on a fresh invite: exactly one `DELETE` is sent.
7. Invite "Kid" (limited, with an email) and accept it from Mailpit in a private window. Back as the owner, Kid appears under Members and is gone from Pending invites.
8. Signed in as a limited member, the Members panel shows no Pending invites section and no red error, and the network log shows no request to `/api/v1/household/invites`.
9. Overview on a household with one owner and no pending invite: "Finish setting up" lists four steps, including "Invite your partner" with a **Set up** link.
10. That link lands on Settings with the invite modal open.
11. With an owner-role invite pending, the step reads "Invite sent — waiting for your partner", its link says **See invite**, and it lands on Settings with no modal.
12. After that owner invite is accepted, Overview's step is ticked, and the whole checklist disappears once the account and budget steps are also done.
13. On Agreements (locked, one owner), the "Invite your partner" link lands on Settings with the modal open.
14. At 360 px wide, a pending row with a long email truncates, and nothing overflows horizontally.
15. With a personal API token (`POST /api/v1/auth/tokens` from the browser session, or `hearthctl token create`), `curl -X DELETE -H "Authorization: Bearer <token>" http://localhost:5173/api/v1/household/invites/<id>` answers `403` `SESSION_REQUIRED`, and the invite is still listed.

The console shows no errors other than the known `favicon.ico` 404. Remove every row the walk created, or name in the verification file the ones that remain.

- [ ] **Step 4: Update the docs, in this change**

- **`docs/FEATURE_TRACKER.md`:**
  - Add a ✅ row in the identity/Settings section: "Pending invites in Settings: list and withdraw". Its notes cite this plan and the verification file, and say that `DELETE` needs a browser session and deletes the row.
  - In the Overview section, rewrite the note that says the partner step "joins the list in the change that exposes pending invites". It now exists; the checklist has four steps.
  - Leave "Telegram invites" ⬜ (milestone 2).
  - **Recount the summary table** by walking the state column with the method the 2026-09-12 note describes (split only on unescaped pipes). Never adjust it by hand.
- **`docs/SYSTEM_DESIGN.md`:** use the `maintaining-system-design` skill.
  - Add both routes with their guards.
  - Add `ListPending` and `Delete` to the `InviteRepository` row of the ports table.
  - State the one definition of "pending".
  - Add the `["household", "invites"]` query key where the frontend's query keys are described.
- **`docs/LEARNING.md`:** add what this work taught, as evidence under an existing pattern where it fits. Pattern 22 and the Overview tests' throw-on-unregistered-route cascade are likely candidates. If nothing broke, say so in one line rather than inventing an entry.
- **`docs/HANDOVER.md`:** one short paragraph under "What to do next": milestone 1 of the partner-invite lobby is built and walked; milestone 2's plan is next.

- [ ] **Step 5: Commit**

```bash
git add docs
git commit -m "docs: partner invite lobby milestone 1 verified and recorded

Claude-Session: https://claude.ai/code/session_01EVA25nn5hrgVDeR3C1m8zA"
```
