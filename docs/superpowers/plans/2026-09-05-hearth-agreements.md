# Marriage Agreements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Marriage → Agreements screen — a versioned living document grouped into sections, where every change goes through propose → every owner signs, removed agreements stay visible and restorable, and a proposal parked for discussion appears on the Retros page.

**Architecture:** Four tables (`agreement_sections`, `agreement_proposals`, `agreements`, `agreement_signatures`) behind one port, one service holding every derived figure, one HTTP group joining the existing `requireCapability(marriage)` + `requireOwner` pair, and a React feature folder whose fetch orchestration lives in one hook. Nothing is ever deleted: a removal is a stamp, and the document's version number is `count(accepted proposals) + 1`, derived on every read. The two writes that touch more than one row — proposing, which also records the proposer's own signature, and signing, which may also apply the change — are each one repository method running one transaction on that transaction's own connection.

**Tech Stack:** Go 1.24 + chi + pgx/v5 + sqlc (queries in `api/internal/adapter/postgres/queries/`), goose migrations, Postgres 17, React 19 + TypeScript + TanStack Query + Tailwind, Vitest + Testing Library, testcontainers for the Go database tests.

**Spec:** `docs/superpowers/specs/2026-09-05-hearth-agreements-design.md` — read it before Task 1. Every decision number referenced below (decision 1 … decision 22) is a numbered item in that document's Decisions section.

## Global Constraints

- **Clean architecture, enforced by `make lint-arch`.** `internal/domain` imports the standard library only; `internal/usecase` may add `internal/domain`; everything else lives under `internal/adapter/**` or `cmd/**`. No pgx, chi or HTTP type crosses out of an adapter. A missing row becomes `domain.ErrNotFound` at the adapter boundary, never `pgx.ErrNoRows`.
- **No service takes an actor parameter to decide whether a caller may act.** Services enforce what is *valid*; middleware enforces who is *asking* (`CLAUDE.md`). The membership id `Propose` and `Sign` take is **stored**, never consulted for permission — the precedent is `PaymentWrite.PaidByMembershipID`.
- **Every 2xx except 204 carries a JSON body**, because `apiFetch` throws on an ok response it cannot parse. This feature has no 204 and no DELETE at all.
- **Fail closed on values you did not construct.** A `switch` over anything arriving from a database column or a request body needs a `default` that refuses. `kind` arrives from both; `status` from a column only.
- **Text caps are counted in runes, never bytes** — `utf8.RuneCountInString`, not `len`. `MaxAgreementBodyLen = 500`, `MaxAgreementNoteLen = 500`, `MaxAgreementParkNoteLen = 500`, `MaxAgreementSectionNameLen = 60`, `MinAgreementOwners = 2`.
- **The signing set is every current owner, evaluated live** (decision 4) — never literally two, and never snapshotted at proposal time.
- **No new dependencies.** Versions stay pinned; floating versions have broken this build twice.
- **`stubFetchRoutes` for every frontend test.** It matches on method *and* URL and throws on an unregistered request; every existing Retros page test gains the agreements route in the same change that mounts the To-discuss block, because an unregistered route throws, TanStack Query absorbs the throw into error state, and a component that renders null on missing data stays green while silently erroring.
- **At least one mutation-checked test per task.** Break the implementation on purpose, watch the named test go red *for the expected reason*, restore. Record which mutation you used in the commit body. An orphaned import gives a build failure, which prints the same word as a test failure — say which you saw.
- **Two breakpoints only, `sm` (640px) and `lg` (1024px)**, `dvh` not `vh` on full-height boxes, and a 44px touch-target floor on interactive controls.
- **Test commands:** `cd api && go test ./... -count=1 -timeout=5m` (needs Docker; on the original machine export `DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock` and `TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`), `cd web && npx vitest run`, and `make lint` before any commit that closes a task. `go` is not on `PATH` in a bare shell here — it lives at `/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin`.
- **Host edits to `web/src/**` do not reliably reach the running dev server.** If a change appears not to compile, `docker restart hearth-web-1` before debugging the code (`docs/HANDOVER.md` §2). This machine also runs two Docker engines — check `lsof` on 5173 before trusting what `localhost` serves.

## File structure

| File | Responsibility | Task |
|---|---|---|
| `api/migrations/00014_agreements.sql` | The four tables, their constraints and two indexes | 1 |
| `api/internal/domain/agreement.go` | Kinds, statuses, caps, validation, the signing-set rule, the display-numbering rule | 2 |
| `api/internal/domain/errors.go` | Fourteen new sentinels, one block | 2 |
| `api/internal/usecase/ports.go` | `AgreementRepository`, its record and write types | 3 |
| `api/internal/usecase/agreement.go` | `AgreementService` — the composed read and the six writes | 3, 4 |
| `api/internal/adapter/postgres/queries/agreements.sql` | Every sqlc query this feature needs | 5, 6 |
| `api/internal/adapter/postgres/agreement_repo.go` | The non-transactional half: document read, proposal read, section writes | 5 |
| `api/internal/adapter/postgres/agreement_write_repo.go` | The transactional half: propose, sign, park, withdraw | 6 |
| `api/internal/adapter/http/agreement_handlers.go` | Seven handlers and their DTOs | 7, 8 |
| `api/internal/adapter/http/errors.go` | Twelve new `MapDomainError` cases | 8 |
| `api/internal/adapter/http/router.go` | Seven routes into the existing marriage group | 7, 8 |
| `web/src/features/marriage/agreementSchemas.ts` | Zod mirrors of the wire DTOs | 9 |
| `web/src/features/marriage/agreementQueryKeys.ts` | The one query key both screens share | 9 |
| `web/src/features/marriage/agreementCopy.ts` | Every string, `joinNames`, `agreementDateLabel`, `historyDateLabel` | 9 |
| `web/src/features/marriage/useAgreements.ts` | One `GET`, six writes, `afterWrite()`, `handleWriteError` | 9 |
| `web/src/features/marriage/AgreementsPage.tsx` | Layout and screen states only; no `apiFetch` | 10 |
| `web/src/routes/router.tsx`, `web/src/features/shell/Sidebar.tsx` | The route under `marriageGuardRoute`, the nav entry, and `validateSearch` on `settingsRoute` | 10 |
| `web/src/features/settings/SettingsPage.tsx`, `MembersPanel.tsx` | The invite deep link's landing half | 10 |
| `web/src/features/marriage/AgreementSectionCard.tsx` | One section: name, count, numbered rows | 11 |
| `web/src/features/marriage/ProposalCard.tsx` | One proposal, its body and its actions | 12 |
| `web/src/features/marriage/ProposeAgreementModal.tsx` | The three-mode editor and its conflict latch | 13 |
| `web/src/features/marriage/NewSectionModal.tsx` | A section name, suggestion chips, and the starter set's landing | 14 |
| `web/src/features/marriage/VersionHistoryModal.tsx` | Accepted changes, the disclosure, and Restore | 15 |
| `web/src/features/marriage/AgreementsToDiscuss.tsx` | The read-only block on the Retros page | 16 |
| `web/src/features/marriage/RetrosPage.tsx` | Mounts the block as a sibling of its no-retros branch | 16 |

## Interfaces, in one place

Every task below copies its signatures from here rather than inventing them. These are the names later tasks depend on.

**Go — `domain` (Task 2)**

```go
type AgreementProposalKind string   // ProposalAdd "add", ProposalEdit "edit", ProposalRemove "remove"
type AgreementProposalStatus string // ProposalPending, ProposalParked, ProposalAccepted, ProposalWithdrawn

func ParseAgreementProposalKind(s string) (AgreementProposalKind, error)
func ParseAgreementProposalStatus(s string) (AgreementProposalStatus, error)
func (s AgreementProposalStatus) IsOpen() bool

func StarterSectionNames() []string
func RequiredSigners(all []Membership) []string
func AwaitingSignature(all []Membership, signed []string) []string
func AgreementsLocked(all []Membership) bool
func AgreementDisplayNumbers(sizes []int) [][]int

type AgreementProposal struct {
    HouseholdID, Kind, SectionID, TargetAgreementID, Body, PreviousBody, Note, ProposedByMembershipID string
}
func (p AgreementProposal) Validate() error
func ValidateAgreementSectionName(name string) error
```

**Go — `usecase` (Task 3)**

```go
type AgreementSectionRecord struct{ ID, Name string; CreatedAt time.Time }
type AgreementRecord struct{ ID, SectionID, Body, AddedByProposalID string; CreatedAt time.Time }
type AgreementProposalRecord struct {
    ID, Kind, Status, SectionID, TargetAgreementID, Body, PreviousBody, Note, ParkNote,
    ProposedByMembershipID string
    CreatedAt              time.Time
    ResolvedAt             *time.Time
    SignedByMembershipIDs  []string
}
type AgreementDocument struct {
    Sections   []AgreementSectionRecord
    Agreements []AgreementRecord          // live only, removed_at IS NULL
    Open       []AgreementProposalRecord  // pending and parked, created_at asc
    Accepted   []AgreementProposalRecord  // accepted only, resolved_at asc
}
type AgreementProposalWrite struct {
    HouseholdID, Kind, SectionID, TargetAgreementID, Body, PreviousBody, Note,
    ProposedByMembershipID string
    CreatedAt              time.Time
}
type AgreementSignatureWrite struct {
    HouseholdID, ProposalID, MembershipID string
    At                                    time.Time
}

type AgreementRepository interface {
    Document(ctx context.Context, householdID string) (AgreementDocument, error)
    Proposal(ctx context.Context, householdID, proposalID string) (AgreementProposalRecord, error)
    CreateSection(ctx context.Context, householdID, name string, createdAt time.Time) (AgreementSectionRecord, error)
    CreateSections(ctx context.Context, householdID string, names []string, createdAt time.Time) ([]AgreementSectionRecord, error)
    CreateProposal(ctx context.Context, in AgreementProposalWrite) (AgreementProposalRecord, error)
    Sign(ctx context.Context, in AgreementSignatureWrite) (AgreementProposalRecord, error)
    Park(ctx context.Context, householdID, proposalID, note string, at time.Time) (AgreementProposalRecord, error)
    Withdraw(ctx context.Context, householdID, proposalID, byMembershipID string, at time.Time) (AgreementProposalRecord, error)
}
```

**Go — `usecase` view types and the service (Tasks 3 and 4)**

The repository returns records; the service returns **views**, which are what the HTTP layer maps to
DTOs. Every write returns the row it touched **and** the whole recomposed document, the shape
`VisionService.Save` already uses (`api/internal/usecase/vision.go:117`) and the spec requires
("Every write returns the whole document, because each moves the version, the `01..N` numbering, the
history list and which proposals are open").

```go
type AgreementOwner struct{ MembershipID, Name string }
type AgreementLine struct{ ID, Body string; Number int }
type AgreementSectionView struct {
    ID, Name string
    Count    int
    Visible  bool
    Agreements []AgreementLine
}
type AgreementProposalView struct {
    ID, Kind, Status, SectionID, SectionName, TargetAgreementID string
    Body, PreviousBody, Note, ParkNote                          string
    ProposedByMembershipID, ProposedByName                      string
    ProposedAt                                                  time.Time
    AwaitingNames                                               []string
    // Every membership id that has signed, so the HTTP layer can decide the
    // viewer's own canAgree. It never reaches the wire: a browser comparing
    // membership ids is the thing canAgree exists to replace.
    SignedByMembershipIDs                                       []string
    TargetChanged                                               bool
}
type AgreementHistoryEntry struct {
    Version                                        int
    ProposalID, Kind, SectionID, SectionName       string
    Body, PreviousBody, Note, ProposedByName       string
    SignedByNames                                  []string
    AcceptedAt                                     time.Time
}
type AgreementsView struct {
    Locked    bool
    Owners    []AgreementOwner
    Version   int
    UpdatedAt *time.Time
    Sections  []AgreementSectionView
    Proposals []AgreementProposalView   // pending and parked only
    History   []AgreementHistoryEntry   // newest first
}

func (s *AgreementService) Get(ctx context.Context, householdID string) (AgreementsView, error)
func (s *AgreementService) Proposal(ctx context.Context, householdID, proposalID string) (AgreementProposalRecord, error)
func (s *AgreementService) CreateSection(ctx context.Context, householdID, name string, at time.Time) (AgreementSectionView, AgreementsView, error)
func (s *AgreementService) SeedStarterSections(ctx context.Context, householdID string, at time.Time) (AgreementsView, error)
func (s *AgreementService) Propose(ctx context.Context, householdID, proposedByMembershipID string, p domain.AgreementProposal, at time.Time) (AgreementProposalView, AgreementsView, error)
func (s *AgreementService) Sign(ctx context.Context, householdID, proposalID, membershipID string, at time.Time) (AgreementProposalView, AgreementsView, error)
func (s *AgreementService) Park(ctx context.Context, householdID, proposalID, note string, at time.Time) (AgreementProposalView, AgreementsView, error)
func (s *AgreementService) Withdraw(ctx context.Context, householdID, proposalID, byMembershipID string, at time.Time) (AgreementProposalView, AgreementsView, error)
```

**One helper makes the write responses possible.** `Get` composes a proposal view inside its walk over
`doc.Open`, but a write must return a proposal that has just become `accepted` or `withdrawn` — rows
that walk excludes in SQL. So the per-proposal composition is factored out and both call it:

```go
func (s *AgreementService) proposalView(
    p AgreementProposalRecord,
    all []domain.Membership,
    names map[string]string,        // membership id -> display name
    liveBody map[string]string,     // live agreement id -> its current body
    sectionNames map[string]string, // section id -> name
) (AgreementProposalView, error)
```

Each write therefore reads back through `Get` after its repository call and runs the written record
through `proposalView`, so the row and the document in one response are composed by the same code.

**TypeScript — the hook (Task 9)**

```ts
export function useAgreements(): {
  data: AgreementsDocument | undefined
  isLoading: boolean
  error: unknown
  reload: () => Promise<void>
  createSection: (name: string) => Promise<{ section: AgreementSection; agreements: AgreementsDocument }>
  seedStarterSet: () => Promise<AgreementsDocument>
  propose: (body: ProposeBody) => Promise<AgreementsDocument>
  agree: (proposalId: string) => Promise<AgreementsDocument>
  park: (proposalId: string, note: string) => Promise<AgreementsDocument>
  withdraw: (proposalId: string) => Promise<AgreementsDocument>
  isProposing: boolean
  isCreatingSection: boolean
}

export function handleWriteError(err: unknown, reload: () => Promise<void>, fallback: string): string
```

`createSection` is the one write that returns more than the document, because "Create & add first
agreement" needs the new section's id to seed the Propose modal. Every other write resolves to the
document alone: both screens re-render off it, and nothing renders a write response's proposal row.

**TypeScript — the wire (Task 9)**

```ts
type AgreementsDocument = {
  locked: boolean
  owners: { membershipId: string; name: string }[]
  version: number
  updatedAt: string | null
  sections: { id: string; name: string; count: number; visible: boolean; agreements: { id: string; number: number; body: string }[] }[]
  proposals: AgreementProposal[]     // pending and parked only
  history: AgreementHistoryEntry[]   // newest first
}
```

## Conventions every task follows

These exist because eight people wrote these tasks and each of them would otherwise pick differently.

- **One copy module, one object.** Every user-visible string in this feature lives in
  `AGREEMENT_COPY` in `agreementCopy.ts`. No `PROPOSAL_CARD_COPY`, no `AGREEMENT_DOCUMENT_COPY`: later
  tasks *append keys* to the one object, and a duplicate key is a TypeScript error, so check before
  typing.
- **`joinNames` joins with `" and "`**, not the design's `&` — one place, so every test asserts
  "Christine and Ibu". `historyDateLabel` is `en-GB` `{ day: "numeric", month: "short", year:
  "numeric" }` → `28 Jun 2026`; `agreementDateLabel` drops the year.
- **`fireEvent`, never `userEvent`.** `@testing-library/user-event` is not in `web/package.json` and
  Global Constraints forbid new dependencies. `fireEvent.change` replaces `userEvent.type`.
- **`AgreementsPage.tsx` owns every piece of page state**: `useMe()`, the three modal booleans
  (`proposeSeed`, `newSectionOpen`, `historyOpen`) and the three header buttons that set them. It
  lands complete in Task 10, with the buttons hidden — not disabled — when `doc.locked`. Tasks 13, 14
  and 15 mount their modals against state that already exists.
- **Because the page calls `useMe()`, every page test stubs `GET /api/v1/auth/me`.** `stubFetchRoutes`
  throws on an unregistered request (`web/src/test/fetchStub.ts`), so a missing stub fails the test
  rather than passing silently.
- **Task 10 writes the shared frontend test helpers** — `DOC_URL`, `renderPage`, `documentFixture`,
  `proposalFixture`, `emptyDoc`, `seededDoc`, `ONE_OWNER` — and every later frontend task reuses them
  by those names.
- **Every response is wrapped.** The wire is `{ "agreements": {…} }` and a write is
  `{ "proposal": {…}, "agreements": {…} }` or `{ "section": {…}, "agreements": {…} }`. Test fixtures
  wrap; the schemas parse the envelope and return the inner object.
- **Every commit step records its mutation in the commit body** — which line you broke, which test
  went red, and whether what you saw was a test failure or a build failure. The Global Constraints
  require it; a one-line commit message does not satisfy them.
- **Router-wide walk floors are re-measured from each walk's own `t.Logf` output**, never bumped by
  arithmetic on the previous number. Three walks exist: the CSRF walk, the unauthenticated walk, and
  `TestOwnerOnlyRoutesRejectALimitedMember` in `household_api_test.go`.

---

### Task 1: The migration

**Files:**
- Create: `api/migrations/00014_agreements.sql`
- Create: `api/internal/adapter/postgres/agreement_schema_test.go`

**Interfaces:**

- Consumes: nothing. This is the first task; the only existing objects it names are `households(id)` and
  `memberships(id)`, both from `00002_identity.sql`.
- Produces: four tables, two indexes, and five constraint names Tasks 5 and 6 map by **string**. Later
  tasks read these identifiers verbatim — a rename here is a silent break there, because a constraint
  mapped by a name that no longer exists falls through to the generic `23505`/`23503` handling:

```
-- Tables and their columns, exactly as later tasks name them:
agreement_sections    (id, household_id, name, created_at)
agreement_proposals   (id, household_id, kind, status, section_id, target_agreement_id,
                       body, previous_body, note, park_note, proposed_by_membership_id,
                       created_at, resolved_at)
agreements            (id, household_id, section_id, body, added_by_proposal_id,
                       removed_by_proposal_id, removed_at, created_at)
agreement_signatures  (proposal_id, membership_id, signed_at)   -- PK (proposal_id, membership_id)

-- Constraint names Task 5's and Task 6's error translation matches on:
agreement_sections_household_id_name_key            -> domain.ErrAgreementSectionNameTaken (decision 19)
agreement_proposals_shape                           -- Validate's rule, as a backstop
agreement_proposals_resolution_matches_status       -- status and resolved_at cannot disagree
agreements_removal_is_whole                         -- a removal is one event, not half of one
agreement_proposals_target_agreement_id_fkey        -- added by ALTER, closing the cycle

-- Indexes:
agreements_household_live_idx                       -- ON agreements (household_id) WHERE removed_at IS NULL
agreement_proposals_household_status_idx            -- ON agreement_proposals (household_id, status)
```

Migrations are applied by every Postgres test — `testsupport.StartPostgres` calls `goose.Up` over
`api/migrations` (`api/internal/testsupport/postgres.go:69`) — so "up runs clean" is proved by any test
that boots a container. **Nothing in Go ever runs `goose down`**, which is why Step 6 runs it by hand.
The test gets its own file, the newest convention here: 00009 was pinned inside `schema_test.go:929`,
00010 got no schema test at all, and 00011 and 00012 each got one of their own
(`telegram_schema_test.go`, `admin_schema_test.go`).

- [ ] **Step 1: Write the failing test**

Create `api/internal/adapter/postgres/agreement_schema_test.go`. Two package-level helpers in
`postgres_test` are shared by every file in it and are used verbatim below — both were read from the
tree while writing this task, so the signatures are current:

- `insertTestHousehold(t *testing.T, db *postgres.DB) string` (`schema_test.go:1018`)
- `insertTestMembership(t *testing.T, db *postgres.DB, householdID, displayName string) string`
  (`transaction_repo_test.go:38`) — it inserts a user and then a membership with `role = 'owner'` and
  all four capabilities, and returns the **membership** id. That is what this test deletes.
- `openTestDB(t *testing.T) *postgres.DB` (`schema_test.go:1029`) — starts a container, runs `goose.Up`,
  registers `db.Close` as cleanup. `db.Pool()` (`pool.go:41`) hands back the `*pgxpool.Pool`.

```go
package postgres_test

import (
	"context"
	"testing"
)

// TestAgreementLogColumnsSurviveTheirMembership proves decision 20's deliberate absence of any ON DELETE
// action on agreement_proposals.proposed_by_membership_id and agreement_signatures.membership_id. Deleting
// the HOUSEHOLD would cascade both sides away and prove nothing, so this deletes the membership row directly:
// CASCADE would silently un-sign an accepted agreement, RESTRICT would make removing an owner impossible
// after their first proposal, and the record of who agreed has to outlive the person leaving.
func TestAgreementLogColumnsSurviveTheirMembership(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t)
	pool := db.Pool()
	householdID := insertTestHousehold(t, db)
	membershipID := insertTestMembership(t, db, householdID, "Andreas")

	// created_at and signed_at are NOT NULL with no default (the migration says why), and an accepted
	// proposal must carry resolved_at or agreement_proposals_resolution_matches_status refuses the row.
	var sectionID string
	if err := pool.QueryRow(ctx, `INSERT INTO agreement_sections (household_id, name, created_at)
		 VALUES ($1, 'Money', now()) RETURNING id`, householdID).Scan(&sectionID); err != nil {
		t.Fatalf("insert section: %v", err)
	}
	var proposalID string
	if err := pool.QueryRow(ctx, `INSERT INTO agreement_proposals (household_id, kind, status, section_id,
		 body, proposed_by_membership_id, created_at, resolved_at)
		 VALUES ($1, 'add', 'accepted', $2, 'We review the budget monthly.', $3, now(), now()) RETURNING id`,
		householdID, sectionID, membershipID).Scan(&proposalID); err != nil {
		t.Fatalf("insert proposal: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agreement_signatures (proposal_id, membership_id, signed_at)
		 VALUES ($1, $2, now())`, proposalID, membershipID); err != nil {
		t.Fatalf("insert signature: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO agreements (household_id, section_id, body,
		 added_by_proposal_id, created_at) VALUES ($1, $2, 'We review the budget monthly.', $3, now())`,
		householdID, sectionID, proposalID); err != nil {
		t.Fatalf("insert agreement: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM memberships WHERE id = $1`, membershipID); err != nil {
		t.Fatalf("deleting the signer's membership must be allowed: %v", err)
	}
	for _, c := range []struct{ what, query string }{
		{"proposal", `SELECT count(*) FROM agreement_proposals WHERE id = $1`},
		{"signature", `SELECT count(*) FROM agreement_signatures WHERE proposal_id = $1`},
		{"agreement", `SELECT count(*) FROM agreements WHERE added_by_proposal_id = $1`},
	} {
		var n int
		if err := pool.QueryRow(ctx, c.query, proposalID).Scan(&n); err != nil {
			t.Fatalf("count %s rows: %v", c.what, err)
		}
		if n != 1 {
			t.Fatalf("%s rows after deleting the membership = %d, want 1", c.what, n)
		}
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
cd api && go test ./internal/adapter/postgres/ -run TestAgreementLogColumns -count=1 -v
```

Expected: a **test** failure, not a build failure — the file compiles, because every symbol it uses
already exists. The container boots, `goose.Up` runs the thirteen migrations that exist, the household
and membership inserts succeed, and the first agreements statement is the one that reddens:

```
    agreement_schema_test.go:NN: insert section: ERROR: relation "agreement_sections" does not exist (SQLSTATE 42P01)
--- FAIL: TestAgreementLogColumnsSurviveTheirMembership
```

- [ ] **Step 3: Write the migration**

Create `api/migrations/00014_agreements.sql`. The comments are part of the deliverable — this file is
where a future editor decides whether to add a column, and every non-obvious choice has to argue for
itself here:

```sql
-- +goose Up
-- No position column anywhere below; ordering is created_at, id, for the reason 00009_retros.sql:34-41 gives
-- (decision 11): the only safe writer is max(position)+1 inside the insert, two owners still collide on it, and
-- no reordering control is drawn -- the column would only create that race.
--
-- A section is a label, and creating one is immediate and unsigned (decision 8). UNIQUE (household_id, name) is
-- decision 19: the adapter maps that CONSTRAINT NAME, above the generic 23505 case, to
-- ErrAgreementSectionNameTaken, so the screen can name what collided.
CREATE TABLE agreement_sections (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid        NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    name         text        NOT NULL,
    -- No DEFAULT now(): "Use starter set" writes four rows in one transaction, and one shared now() would tie
    -- them into a random uuid order. The writer stamps them strictly apart.
    created_at   timestamptz NOT NULL,
    UNIQUE (household_id, name)
);
-- The append-only log of every change ever proposed (decision 9): nothing is deleted and no content column is
-- rewritten -- only status, resolved_at and park_note move, so there is no updated_at.
CREATE TABLE agreement_proposals (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    -- Parsed in Go on the way out; every CHECK here is a backstop, never the enforcement.
    kind         text NOT NULL CHECK (kind IN ('add', 'edit', 'remove')),
    status       text NOT NULL CHECK (status IN ('pending', 'parked', 'accepted', 'withdrawn')),
    -- Copied from the target on an edit or a remove, so a pending edit whose target is removed has a card.
    section_id   uuid NOT NULL REFERENCES agreement_sections(id),
    target_agreement_id uuid,          -- NULL for an add; its FK is added below
    body          text NOT NULL DEFAULT '',
    previous_body text NOT NULL DEFAULT '',  -- the target's wording when proposed (decision 13)
    note          text NOT NULL DEFAULT '',
    park_note     text NOT NULL DEFAULT '',
    -- A log column with NO foreign key, which is what decision 20's "no ON DELETE action" has to mean in
    -- Postgres: a bare REFERENCES defaults to NO ACTION, members are hard-deleted (queries/identity.sql:74), and
    -- an FK would then refuse to remove an owner who had ever proposed anything -- the outcome decision 20
    -- rejects RESTRICT for. CASCADE, which retro_action_assignees uses, would silently un-sign an accepted
    -- agreement. The id is verified at write time through memberships (this household, role = 'owner'), and
    -- outlives the person leaving because the record of who agreed has to.
    proposed_by_membership_id uuid NOT NULL,
    created_at  timestamptz NOT NULL,
    resolved_at timestamptz,           -- NULL means open, retros.completed_at's shape
    CONSTRAINT agreement_proposals_resolution_matches_status  -- the two cannot disagree
        CHECK ((status IN ('pending', 'parked')) = (resolved_at IS NULL)),
    -- Validate's rule again, for statements written by hand. ELSE false so an unmatched kind fails closed: a
    -- CHECK accepts NULL.
    CONSTRAINT agreement_proposals_shape CHECK (
        CASE kind
        WHEN 'add'    THEN target_agreement_id IS NULL     AND body <> '' AND previous_body =  ''
        WHEN 'edit'   THEN target_agreement_id IS NOT NULL AND body <> '' AND previous_body <> ''
        WHEN 'remove' THEN target_agreement_id IS NOT NULL AND body =  '' AND previous_body <> ''
        ELSE false END
    )
);
-- The living document, and a row stays in it forever: removal is a stamp (decision 9) -- "it stays in Version
-- history, so you can always see it was there and restore it later".
CREATE TABLE agreements (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id uuid NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    section_id   uuid NOT NULL REFERENCES agreement_sections(id),
    body         text NOT NULL,
    -- Never NULL: propose -> sign is the only path -- "everything here is here because you both agreed".
    added_by_proposal_id   uuid NOT NULL REFERENCES agreement_proposals(id),
    removed_by_proposal_id uuid REFERENCES agreement_proposals(id),
    removed_at             timestamptz,
    created_at             timestamptz NOT NULL,
    CONSTRAINT agreements_removal_is_whole   -- a removal is one event, not half of one
        CHECK ((removed_at IS NULL) = (removed_by_proposal_id IS NULL))
);
-- Closes the cycle, at the default NO ACTION not RESTRICT: a household delete cascades into both sides of it in
-- one statement, and only NO ACTION waits for the end of that statement to check.
ALTER TABLE agreement_proposals
    ADD CONSTRAINT agreement_proposals_target_agreement_id_fkey
    FOREIGN KEY (target_agreement_id) REFERENCES agreements(id);

-- One owner's Agree. The primary key makes a double-clicked Agree idempotent (decision 16): the write is an
-- upsert and signed_at keeps the first stamp. Nothing deletes a signature either -- a departed owner's is a true
-- record that stops counting, because the set is every CURRENT owner (decision 4).
CREATE TABLE agreement_signatures (
    proposal_id   uuid NOT NULL REFERENCES agreement_proposals(id) ON DELETE CASCADE,
    membership_id uuid NOT NULL,       -- a log column with no FK, as above
    signed_at     timestamptz NOT NULL,
    PRIMARY KEY (proposal_id, membership_id)   -- scoped to a household via the proposal
);

-- Live reads carry the predicate rather than filter a growing tail of removed rows; the second index serves
-- Document's split of the log.
CREATE INDEX agreements_household_live_idx ON agreements (household_id) WHERE removed_at IS NULL;
CREATE INDEX agreement_proposals_household_status_idx ON agreement_proposals (household_id, status);

-- +goose Down
-- The ALTER goes first because the two tables name each other -- agreements.added_by_proposal_id points at
-- agreement_proposals and the constraint below points back -- so while it stands, whichever table is dropped
-- first is depended on by the other and Postgres refuses. Breaking the cycle leaves an ordinary child-first order.
ALTER TABLE agreement_proposals DROP CONSTRAINT agreement_proposals_target_agreement_id_fkey;  -- first
DROP TABLE agreement_signatures;
DROP TABLE agreements;
DROP TABLE agreement_proposals;
DROP TABLE agreement_sections;
```

No `GRANT` line belongs in this file. `make migrate` runs the `readonly-role` one-shot straight after
goose, and `deploy/readonly-role.sql:33-41` already covers a table created later: `GRANT SELECT ON ALL
TABLES` re-runs over the four new ones, and the `ALTER DEFAULT PRIVILEGES FOR ROLE hearth` beneath it
names this exact migration in its own comment. So the admin database browse sees the four tables with
nothing added here.

- [ ] **Step 4: Run the test and watch it pass**

```bash
cd api && go test ./internal/adapter/postgres/ -run TestAgreementLogColumns -count=1 -v
```

Expected: `--- PASS: TestAgreementLogColumnsSurviveTheirMembership`, and `ok
github.com/andreasoentoro/hearth/api/internal/adapter/postgres`.

- [ ] **Step 5: Mutation-check it, on both log columns**

Decision 20 makes the same claim about two columns, so break each one. Restore between them and rerun to
confirm green before moving on. Both are **test** failures — the migration still compiles as SQL, and Go
never sees it.

**(a) The signature's membership column.** In `agreement_signatures`, change

```sql
    membership_id uuid NOT NULL,       -- a log column with no FK, as above
```

to

```sql
    membership_id uuid NOT NULL REFERENCES memberships(id) ON DELETE CASCADE,
```

and rerun. Expected red, on the second of the three counts — nothing references `agreement_signatures`,
so the cascade takes only that row and the `DELETE` itself still succeeds:

```
    agreement_schema_test.go:NN: signature rows after deleting the membership = 0, want 1
```

**(b) The proposal's proposer column.** Restore (a) first, then change

```sql
    proposed_by_membership_id uuid NOT NULL,
```

to

```sql
    proposed_by_membership_id uuid NOT NULL REFERENCES memberships(id) ON DELETE CASCADE,
```

and rerun. This one reddens **earlier and with different text**, and the reason is worth reading before
you decide the mutation was wrong: the cascade tries to delete the proposal row, and `agreements
.added_by_proposal_id` — an inline `REFERENCES`, so `NO ACTION`, checked at end of statement — still
points at it. The `DELETE FROM memberships` never completes:

```
    agreement_schema_test.go:NN: deleting the signer's membership must be allowed: ERROR: update or delete on table "agreement_proposals" violates foreign key constraint "agreements_added_by_proposal_id_fkey" on table "agreements" (SQLSTATE 23503)
```

That is the correct red: the test's very first claim — that removing an owner is allowed at all — is what
the mutation breaks. Restore, rerun, confirm green.

- [ ] **Step 6: Prove Down runs clean, by hand**

No Go test runs `goose down`, so this is the only check the Down block ever gets. With the stack up
(`make dev` in another shell, or `make up`):

```bash
make migrate && make migrate-down && make migrate
```

Expected: all three exit 0. The middle one is the one being tested — `make migrate-down` runs
`goose -dir ./migrations postgres "$DATABASE_URL" down`, which rolls back exactly one migration, this
one. Read its output rather than trusting the exit code alone: what must **not** appear — goose wraps
the server's message in its own failure prefix, so look for the text **contained** in the line rather
than a line equal to it — is

```
ERROR: cannot drop table agreements because other objects depend on it (SQLSTATE 2BP01)
```

Reordering the `DROP`s above the `ALTER` produces exactly that, and `agreements` rather than
`agreement_proposals` is which table trips first: `DROP TABLE agreement_signatures` still succeeds,
because nothing references it, and `DROP TABLE agreements` is then the statement
`agreement_proposals_target_agreement_id_fkey` still points at. That is the point of the ordering
comment. The third command re-applies 00014 so the running stack is left where it started.

- [ ] **Step 7: Commit**

```bash
git add api/migrations/00014_agreements.sql api/internal/adapter/postgres/agreement_schema_test.go
git commit -m "feat(agreements): four tables, the cycle between two of them, and the columns that keep no FK

Two mutation checks on decision 20, both test failures, one per log column.
Adding REFERENCES memberships(id) ON DELETE CASCADE to
agreement_signatures.membership_id reddened
TestAgreementLogColumnsSurviveTheirMembership on 'signature rows after
deleting the membership = 0, want 1'. Adding the same clause to
agreement_proposals.proposed_by_membership_id reddened it earlier and
differently -- 23503 on agreements_added_by_proposal_id_fkey at the DELETE
itself -- because agreements still points at the proposal the cascade tries
to take."
```

---

### Task 2: Domain rules

**Files:**
- Create: `api/internal/domain/agreement.go`, `api/internal/domain/agreement_test.go`
- Modify: `api/internal/domain/errors.go` — one block appended after `ErrAdminLocked` on line 214, before
  the closing `)` on line 215 (both line numbers read from the tree while writing this task)

**Interfaces:**

- Consumes: nothing outside the standard library. `domain` imports the standard library only, and
  `make lint-arch` fails the build if this file reaches for anything else. It does use `Membership`,
  `RoleOwner` and `RoleLimited`, which already live in this same package (`identity.go:8-9`, `:93-99`).
- Produces, copied verbatim from the plan header's interface map. Later tasks call exactly these:

```go
type AgreementProposalKind string   // ProposalAdd "add", ProposalEdit "edit", ProposalRemove "remove"
type AgreementProposalStatus string // ProposalPending, ProposalParked, ProposalAccepted, ProposalWithdrawn

func ParseAgreementProposalKind(s string) (AgreementProposalKind, error)
func ParseAgreementProposalStatus(s string) (AgreementProposalStatus, error)
func (s AgreementProposalStatus) IsOpen() bool

func StarterSectionNames() []string
func RequiredSigners(all []Membership) []string
func AwaitingSignature(all []Membership, signed []string) []string
func AgreementsLocked(all []Membership) bool
func AgreementDisplayNumbers(sizes []int) [][]int

type AgreementProposal struct {
    HouseholdID, Kind, SectionID, TargetAgreementID, Body, PreviousBody, Note, ProposedByMembershipID string
}
func (p AgreementProposal) Validate() error
func ValidateAgreementSectionName(name string) error
```

  plus the five caps, as untyped integer constants:

```go
const (
    MaxAgreementBodyLen        = 500
    MaxAgreementNoteLen        = 500
    MaxAgreementParkNoteLen    = 500
    MaxAgreementSectionNameLen = 60
    MinAgreementOwners         = 2
)
```

  and **fourteen** sentinels in `errors.go`, which is what the header's file table says this task adds:

```go
ErrAgreementsNeedTwoOwners
ErrAgreementChanged
ErrAgreementNotOpen
ErrAgreementSectionNameTaken
ErrAgreementSectionNameRequired
ErrAgreementSectionNameTooLong
ErrAgreementBodyRequired
ErrAgreementBodyTooLong
ErrAgreementNoteTooLong
ErrAgreementParkNoteTooLong
ErrAgreementProposalShapeInvalid
ErrAgreementEditUnchanged
ErrUnknownAgreementProposalKind      // deliberately unmapped in Task 8
ErrUnknownAgreementProposalStatus    // deliberately unmapped in Task 8
```

  Fourteen here minus those last two is **twelve** `MapDomainError` cases in Task 8, which is what the
  header's file table says that task adds. If you find yourself writing a thirteenth mapper case, one of
  the two unmapped sentinels has leaked out of a column and into a request path — that is a bug in the
  handler, not a missing case (decision 21).

- [ ] **Step 1: Write the failing tests**

Create `api/internal/domain/agreement_test.go`. Table tests, no doubles — this package has nothing to
double:

```go
package domain_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// proposal is the valid fixture for one kind, with any mutation applied.
func proposal(kind string, mutate ...func(*domain.AgreementProposal)) domain.AgreementProposal {
	p := domain.AgreementProposal{HouseholdID: "h1", Kind: kind, SectionID: "s1",
		Body: "We review the budget on the first Sunday.", ProposedByMembershipID: "m1"}
	switch kind {
	case "edit":
		p.SectionID, p.TargetAgreementID, p.PreviousBody = "", "a1", "We review the budget on Sundays."
	case "remove":
		p.SectionID, p.TargetAgreementID, p.Body, p.PreviousBody = "", "a1", "", "We review the budget on Sundays."
	}
	for _, m := range mutate {
		m(&p)
	}
	return p
}

// Both parsers refuse the empty value, a capitalised one, a kind that sounds plausible, one with a trailing
// space, and the two statuses decision 6 deliberately lacks -- the refusing default is the whole point of them.
func TestAgreementParsersRefuseWhatNoMigrationAllows(t *testing.T) {
	for _, s := range []string{"", "Add", "delete", "remove ", "declined", "expired"} {
		if _, err := domain.ParseAgreementProposalKind(s); !errors.Is(err, domain.ErrUnknownAgreementProposalKind) {
			t.Errorf("ParseAgreementProposalKind(%q) err = %v, want ErrUnknownAgreementProposalKind", s, err)
		}
		if _, err := domain.ParseAgreementProposalStatus(s); !errors.Is(err, domain.ErrUnknownAgreementProposalStatus) {
			t.Errorf("ParseAgreementProposalStatus(%q) err = %v, want ErrUnknownAgreementProposalStatus", s, err)
		}
	}
	for _, s := range []string{"add", "edit", "remove"} {
		if got, err := domain.ParseAgreementProposalKind(s); err != nil || string(got) != s {
			t.Errorf("ParseAgreementProposalKind(%q) = %q, %v; want %q, nil", s, got, err, s)
		}
	}
	for _, c := range []struct {
		s    string
		open bool
	}{{"pending", true}, {"parked", true}, {"accepted", false}, {"withdrawn", false}} {
		got, err := domain.ParseAgreementProposalStatus(c.s)
		if err != nil {
			t.Fatalf("ParseAgreementProposalStatus(%q): %v", c.s, err)
		}
		if got.IsOpen() != c.open {
			t.Errorf("%q.IsOpen() = %v, want %v", c.s, got.IsOpen(), c.open)
		}
	}
}

func TestAgreementProposalValidate(t *testing.T) {
	// 500 runes of Chinese is 1500 bytes: the cap counts runes, so this passes.
	body := strings.Repeat("承", domain.MaxAgreementBodyLen)
	cases := []struct {
		name string
		in   domain.AgreementProposal
		want error
	}{
		{"a valid add", proposal("add"), nil},
		{"a valid edit", proposal("edit"), nil},
		{"a valid remove", proposal("remove"), nil},
		{"a body at the rune cap", proposal("add", func(p *domain.AgreementProposal) { p.Body = body }), nil},
		{"a body trimmed back under the cap", proposal("add", func(p *domain.AgreementProposal) { p.Body = " " + body + "\n" }), nil},
		{"a body one rune over", proposal("add", func(p *domain.AgreementProposal) { p.Body = body + "承" }), domain.ErrAgreementBodyTooLong},
		{"a blank body", proposal("add", func(p *domain.AgreementProposal) { p.Body = "   " }), domain.ErrAgreementBodyRequired},
		{"a note at the rune cap", proposal("add", func(p *domain.AgreementProposal) {
			p.Note = strings.Repeat("承", domain.MaxAgreementNoteLen)
		}), nil},
		{"a note one rune over", proposal("add", func(p *domain.AgreementProposal) {
			p.Note = strings.Repeat("承", domain.MaxAgreementNoteLen+1)
		}), domain.ErrAgreementNoteTooLong},
		{"an add with no section", proposal("add", func(p *domain.AgreementProposal) { p.SectionID = "" }), domain.ErrAgreementProposalShapeInvalid},
		{"an edit with no target", proposal("edit", func(p *domain.AgreementProposal) { p.TargetAgreementID = "" }), domain.ErrAgreementProposalShapeInvalid},
		{"a remove with no previous body", proposal("remove", func(p *domain.AgreementProposal) { p.PreviousBody = "" }), domain.ErrAgreementProposalShapeInvalid},
		{"an edit that changes nothing", proposal("edit", func(p *domain.AgreementProposal) { p.Body = p.PreviousBody }), domain.ErrAgreementEditUnchanged},
		{"a kind no migration allows", proposal("add", func(p *domain.AgreementProposal) { p.Kind = "declined" }), domain.ErrUnknownAgreementProposalKind},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.in.Validate(); !errors.Is(err, c.want) {
				t.Fatalf("Validate() = %v, want %v", err, c.want)
			}
		})
	}
}

func TestValidateAgreementSectionName(t *testing.T) {
	name := strings.Repeat("承", domain.MaxAgreementSectionNameLen)
	cases := []struct {
		in   string
		want error
	}{
		{"Money", nil},
		{name, nil},
		{"  " + name + " ", nil}, // trimmed before it is measured
		{name + "承", domain.ErrAgreementSectionNameTooLong},
		{"   ", domain.ErrAgreementSectionNameRequired},
	}
	for _, c := range cases {
		if err := domain.ValidateAgreementSectionName(c.in); !errors.Is(err, c.want) {
			t.Fatalf("ValidateAgreementSectionName(%q) = %v, want %v", c.in, err, c.want)
		}
	}
}

// MaxAgreementParkNoteLen is DELIBERATELY not asserted here. Nothing in this package reads it -- the park
// note is capped by AgreementService.Park (Task 4) -- and no domain-level assertion can tell a separate
// constant from `MaxAgreementParkNoteLen = MaxAgreementNoteLen`, because an alias is also 500. The one
// assertion that separates them is Task 4's: Park with a 501-rune note returns ErrAgreementParkNoteTooLong
// and NOT ErrAgreementNoteTooLong. If you are here to add a `!= 500` check, it would pass under the alias
// and prove nothing.

// The fixtures carry a limited member on purpose: without one, these three functions are tested minus the
// filter that is all they do. This is also the ONLY place AwaitingSignature's ordering claim is pinned --
// the service tests cannot pin it, because membershipDouble.List ranges a Go map
// (api/internal/usecase/testdouble_test.go:304) and hands back owners in a different order each run.
func TestOwnerHelpersIgnoreLimitedMembers(t *testing.T) {
	owner := func(id string) domain.Membership { return domain.Membership{ID: id, Role: domain.RoleOwner} }
	limited := domain.Membership{ID: "m9", Role: domain.RoleLimited}
	two := []domain.Membership{owner("m1"), limited, owner("m2")}

	if got := domain.RequiredSigners(two); !slices.Equal(got, []string{"m1", "m2"}) {
		t.Fatalf("RequiredSigners = %v, want [m1 m2]", got)
	}
	// Three owners, the middle one signed: the survivors come back in RequiredSigners' order, not the
	// order the signatures arrived in. "needs Christine and Ibu" is built from this slice.
	three := []domain.Membership{owner("m1"), limited, owner("m2"), owner("m3")}
	if got := domain.AwaitingSignature(three, []string{"m2"}); !slices.Equal(got, []string{"m1", "m3"}) {
		t.Fatalf("AwaitingSignature = %v, want [m1 m3]", got)
	}
	// A signature held by someone who is no longer an owner is ignored, never deleted (decision 4).
	if got := domain.AwaitingSignature(two, []string{"m1", "m9"}); !slices.Equal(got, []string{"m2"}) {
		t.Fatalf("AwaitingSignature ignoring a non-owner's signature = %v, want [m2]", got)
	}
	if domain.AgreementsLocked(two) {
		t.Fatal("two owners must not be locked")
	}
	if !domain.AgreementsLocked([]domain.Membership{owner("m1"), limited}) {
		t.Fatal("one owner and a limited member is locked: a limited member can never be asked to sign")
	}
	if got := domain.StarterSectionNames(); !slices.Equal(got, []string{"Money", "Conflict", "Home & kids", "Us"}) {
		t.Fatalf("StarterSectionNames = %v", got)
	}
}

// Numbering runs continuously across sections -- the first ends at 02 and the third starts at 03 -- and an
// empty section takes no number at all.
func TestAgreementDisplayNumbersRunAcrossSections(t *testing.T) {
	got := fmt.Sprint(domain.AgreementDisplayNumbers([]int{2, 0, 3}))
	if got != "[[1 2] [] [3 4 5]]" {
		t.Fatalf("AgreementDisplayNumbers([2 0 3]) = %s, want [[1 2] [] [3 4 5]]", got)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
cd api && go test ./internal/domain/ -run 'TestAgreement|TestValidateAgreementSectionName|TestOwnerHelpers' -count=1 -v
```

Expected: a **build** failure, not a test failure — no test binary is produced, so `-run` never filters
anything and `-v` prints nothing. The output is the type checker's, one line per undefined *use* in
source order (so the first three are all `domain.AgreementProposal`, from the `proposal` helper), capped
at ten with `too many errors`:

```
# github.com/andreasoentoro/hearth/api/internal/domain_test [github.com/andreasoentoro/hearth/api/internal/domain.test]
internal/domain/agreement_test.go:NN:NN: undefined: domain.AgreementProposal
...
internal/domain/agreement_test.go:NN:NN: too many errors
FAIL	github.com/andreasoentoro/hearth/api/internal/domain [build failed]
```

`[build failed]` beside `FAIL` is how you tell the two apart. Write "build failure" in the commit body
for this step; the mutations in Step 5 are test failures, and the commit body says both.

- [ ] **Step 3: Write the implementation**

Create `api/internal/domain/agreement.go`:

```go
package domain

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// The caps, counted in RUNES below and never in bytes: len() is bytes, and a household writing Chinese would
// get a third of what the field promises -- the mistake MaxVisionThemeLen shipped with once.
const (
	MaxAgreementBodyLen = 500
	MaxAgreementNoteLen = 500
	// Its own constant, never an alias of the note's: they are different fields on different screens, and one
	// cap serving both would have to move for both. Nothing in this package reads it -- AgreementService.Park
	// enforces it and ErrAgreementParkNoteTooLong is its refusal -- so it is declared here, beside its
	// siblings, rather than in usecase where a domain cap does not belong.
	MaxAgreementParkNoteLen    = 500
	MaxAgreementSectionNameLen = 60
	// A minimum, never a maximum: nothing caps a household at two owners, and two is the floor below which
	// nobody can be asked to sign (decision 1).
	MinAgreementOwners = 2
)

// AgreementProposalKind is which of three changes a proposal asks for.
type AgreementProposalKind string

const (
	ProposalAdd    AgreementProposalKind = "add"
	ProposalEdit   AgreementProposalKind = "edit"
	ProposalRemove AgreementProposalKind = "remove"
)

// ParseAgreementProposalKind refuses a kind this code did not construct. A kind arrives from a request body as
// well as a column, and the HTTP handler calls this itself to answer 422 (decision 21), so a corrupt row and a
// caller's typo never share an answer. A fourth kind needs a migration as well as a case here.
func ParseAgreementProposalKind(s string) (AgreementProposalKind, error) {
	switch k := AgreementProposalKind(s); k {
	case ProposalAdd, ProposalEdit, ProposalRemove:
		return k, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownAgreementProposalKind, s)
	}
}

// AgreementProposalStatus is where a proposal stands. There is no fifth: no decline, because an unanswered
// proposal is a conversation that has not happened yet, and no expiry, because silently deleting what one
// partner asked for is the harshest thing this feature could do (decision 6).
type AgreementProposalStatus string

const (
	ProposalPending   AgreementProposalStatus = "pending"
	ProposalParked    AgreementProposalStatus = "parked"
	ProposalAccepted  AgreementProposalStatus = "accepted"
	ProposalWithdrawn AgreementProposalStatus = "withdrawn"
)

// ParseAgreementProposalStatus refuses a status this code did not construct. A status only ever arrives from a
// column, so a refusal here is a corrupt row rather than anyone's mistake.
func ParseAgreementProposalStatus(s string) (AgreementProposalStatus, error) {
	switch st := AgreementProposalStatus(s); st {
	case ProposalPending, ProposalParked, ProposalAccepted, ProposalWithdrawn:
		return st, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownAgreementProposalStatus, s)
	}
}

// IsOpen is pending-or-parked in one function, so a fifth status cannot read as open at three call sites and
// closed at the fourth.
func (s AgreementProposalStatus) IsOpen() bool {
	return s == ProposalPending || s == ProposalParked
}

// StarterSectionNames is what "Use starter set" seeds: four labels and no agreements at all, so "everything
// here is here because you both agreed" stays literally true (decision 17). A fresh slice per call -- a caller
// must not be able to rename this package's own copy.
func StarterSectionNames() []string {
	return []string{"Money", "Conflict", "Home & kids", "Us"}
}

// RequiredSigners is every CURRENT owner's membership id (decision 4). ValidateMembershipChange refuses
// CapMarriage to a limited member, so nobody else can ever be asked to sign.
func RequiredSigners(all []Membership) []string {
	ids := make([]string, 0, len(all))
	for _, m := range all {
		if m.Role == RoleOwner {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

// AwaitingSignature is the owners yet to sign, in RequiredSigners' order -- what the card's "needs Christine"
// is built from. The order is load-bearing and pinned by this package's own test, because the service tests
// cannot pin it: their membership double ranges a Go map. A signature held by someone who is no longer an
// owner is ignored here, never deleted: the record of who agreed outlives their membership.
func AwaitingSignature(all []Membership, signed []string) []string {
	has := make(map[string]bool, len(signed))
	for _, id := range signed {
		has[id] = true
	}
	out := make([]string, 0, len(all))
	for _, id := range RequiredSigners(all) {
		if !has[id] {
			out = append(out, id)
		}
	}
	return out
}

// AgreementsLocked is decision 1's gate, named once so the read path's "is this screen locked" and every write
// path's "may this write happen" cannot drift apart.
func AgreementsLocked(all []Membership) bool {
	return len(RequiredSigners(all)) < MinAgreementOwners
}

// AgreementDisplayNumbers is the design's 01..12. sizes[i] is section i's live count, and the numbering runs
// CONTINUOUSLY across sections -- Money ends at 04 and Conflict starts at 05 -- which is why this takes the
// whole document's shape rather than one section's. Integers, never pre-padded strings: the zero padding is
// presentation and the browser owns it. Derived on every read; storing a number would mean renumbering every
// later agreement on every removal (decision 11).
func AgreementDisplayNumbers(sizes []int) [][]int {
	out := make([][]int, 0, len(sizes))
	// Declared OUTSIDE the loop: this single counter is the whole of the continuity rule. Moved inside, every
	// section restarts at 1 and the document numbers 01,02 / 01,02,03.
	next := 1
	for _, size := range sizes {
		// max(size, 0): the caller counts its own rows, so a negative is a bug -- and make would panic on it.
		numbers := make([]int, 0, max(size, 0))
		for i := 0; i < size; i++ {
			numbers = append(numbers, next)
			next++
		}
		out = append(out, numbers)
	}
	return out
}

// AgreementProposal is one proposed change, before any row exists. The service stamps HouseholdID and
// ProposedByMembershipID from the route and the session, never from a request body.
type AgreementProposal struct {
	HouseholdID            string
	Kind                   string
	SectionID              string
	TargetAgreementID      string
	Body                   string
	PreviousBody           string
	Note                   string
	ProposedByMembershipID string
}

// Validate is every rule that needs no database, run before any repository call so an invalid proposal writes
// nothing. It never rewrites a field -- the service trims on the way in, so what is stored is what was
// validated -- and it measures the trimmed text for that same reason: a 500-rune body with a trailing newline
// must not be refused for a length the stored row will not have.
func (p AgreementProposal) Validate() error {
	if utf8.RuneCountInString(strings.TrimSpace(p.Note)) > MaxAgreementNoteLen {
		return ErrAgreementNoteTooLong
	}
	// Fail closed: an unrecognised kind is refused rather than falling through to a default shape. It cannot
	// arrive from a request -- the handler parses that itself and answers 422 -- so the default arm below is a
	// bug, and an unmapped sentinel logging a 500 is the right answer to one.
	switch AgreementProposalKind(p.Kind) {
	case ProposalAdd:
		// PreviousBody is the target's wording, and an add has no target.
		if p.SectionID == "" || p.TargetAgreementID != "" || p.PreviousBody != "" {
			return ErrAgreementProposalShapeInvalid
		}
		return validateAgreementBody(p.Body)
	case ProposalEdit:
		// No caller-supplied section: CreateProposal copies the target's own, in the statement that verifies it.
		if p.TargetAgreementID == "" || p.SectionID != "" || p.PreviousBody == "" {
			return ErrAgreementProposalShapeInvalid
		}
		if err := validateAgreementBody(p.Body); err != nil {
			return err
		}
		// The version number is a promise that something happened.
		if strings.TrimSpace(p.Body) == strings.TrimSpace(p.PreviousBody) {
			return ErrAgreementEditUnchanged
		}
		return nil
	case ProposalRemove:
		if p.TargetAgreementID == "" || p.SectionID != "" || p.PreviousBody == "" ||
			strings.TrimSpace(p.Body) != "" {
			return ErrAgreementProposalShapeInvalid
		}
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrUnknownAgreementProposalKind, p.Kind)
	}
}

// validateAgreementBody is the body's own two rules, shared by add and edit so the two kinds cannot drift
// apart on what a body may be.
func validateAgreementBody(body string) error {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return ErrAgreementBodyRequired
	}
	if utf8.RuneCountInString(trimmed) > MaxAgreementBodyLen {
		return ErrAgreementBodyTooLong
	}
	return nil
}

// ValidateAgreementSectionName is the New-section modal's rule. There is no cap on how many sections a
// household may have: a count cap is a check-then-write two owners can both pass.
func ValidateAgreementSectionName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ErrAgreementSectionNameRequired
	}
	if utf8.RuneCountInString(trimmed) > MaxAgreementSectionNameLen {
		return ErrAgreementSectionNameTooLong
	}
	return nil
}
```

Then add this block to `api/internal/domain/errors.go`, immediately after `ErrAdminLocked` (line 214) and
before the closing `)` (line 215). Fourteen sentinels in the file's existing style, a comment wherever the
choice is not obvious:

```go
	// --- Agreements ---

	// ErrAgreementsNeedTwoOwners is decision 1's gate, and it sits here beside ErrLastOwner because it is a
	// fact about the owner set rather than part of any port's contract. Every write refuses with it while a
	// household has fewer than MinAgreementOwners owners.
	ErrAgreementsNeedTwoOwners = errors.New("agreements need at least two owners")

	// ErrAgreementChanged is a target that moved, went, or no longer reads the proposal's previous_body.
	// Deliberately not ErrNotFound: "it vanished" and "someone changed it" are different things to be told,
	// and ErrNotFound on these routes means the proposal row itself.
	ErrAgreementChanged = errors.New("the agreement this proposal targets has changed")

	// ErrAgreementNotOpen is a sign, park or withdraw against a resolved proposal -- ordinarily the last
	// signer double-clicking Agree. It means "reload, this was settled", not "try again", and it pairs with
	// AgreementProposalStatus.IsOpen. The wire code it maps to is AGREEMENT_PROPOSAL_RESOLVED, deliberately
	// worded from the caller's side rather than this sentinel's.
	ErrAgreementNotOpen = errors.New("this proposal is no longer open")

	// Sections are never deleted, so a name is never freed again (decision 19).
	ErrAgreementSectionNameTaken    = errors.New("that section name is already used")
	ErrAgreementSectionNameRequired = errors.New("a section needs a name")
	ErrAgreementSectionNameTooLong  = errors.New("a section name is too long")

	ErrAgreementBodyRequired = errors.New("an agreement needs a body")
	ErrAgreementBodyTooLong  = errors.New("an agreement body is too long")

	// The two notes get a sentinel each rather than sharing one, so every 422 can name the field the screen
	// has to highlight -- and so that MaxAgreementParkNoteLen quietly becoming an alias of MaxAgreementNoteLen
	// is visible somewhere. AgreementService.Park's test is that somewhere; no domain assertion can see it.
	ErrAgreementNoteTooLong     = errors.New("a proposal note is too long")
	ErrAgreementParkNoteTooLong = errors.New("a discussion note is too long")

	// ErrAgreementProposalShapeInvalid covers every wrong combination of the four content fields -- an add
	// with no section, an edit with no target, a remove with no previous body -- as one sentinel, the way
	// ErrTransactionAccountsInvalid does: the modal sends one of three complete shapes, so a mismatch is a
	// hand-built request, and four codes would only tell it which field to try next.
	ErrAgreementProposalShapeInvalid = errors.New("this proposal's fields do not match its kind")

	// Separate from the shape refusal, because there the shape is fine and the screen says something
	// different: the version number is a promise that something happened.
	ErrAgreementEditUnchanged = errors.New("an edit must change the wording")

	// Neither of these gets a MapDomainError case (Task 8). A bad kind in a request body is answered 422 by
	// the handler's own parser (decision 21), so both can only reach the mapper from a database column --
	// where a logged 500 is the right answer to an impossible row.
	ErrUnknownAgreementProposalKind   = errors.New("unknown agreement proposal kind")
	ErrUnknownAgreementProposalStatus = errors.New("unknown agreement proposal status")
```

- [ ] **Step 4: Run the tests and watch them pass**

```bash
cd api && go test ./internal/domain/ -run 'TestAgreement|TestValidateAgreementSectionName|TestOwnerHelpers' -count=1 -v
```

Expected: PASS, all five test functions —
`TestAgreementParsersRefuseWhatNoMigrationAllows`, `TestAgreementProposalValidate`,
`TestValidateAgreementSectionName`, `TestOwnerHelpersIgnoreLimitedMembers`,
`TestAgreementDisplayNumbersRunAcrossSections` — with all fourteen subtests of
`TestAgreementProposalValidate` named in the output. Then run the whole package once, unfiltered, to be
sure nothing else in `domain` reddened: `go test ./internal/domain/ -count=1`.

- [ ] **Step 5: Mutation-check three claims**

Three rules here carry the feature and none of them is checked by the compiler. Break each one, watch the
named test go red with the text below, restore, and rerun before the next. All three are **test**
failures — the package still builds, because none of these mutations removes a symbol.

**(a) The owner filter.** In `RequiredSigners`, delete the `if m.Role == RoleOwner {` guard (and its
closing brace) so every membership is appended:

```
    agreement_test.go:NN: RequiredSigners = [m1 m9 m2], want [m1 m2]
--- FAIL: TestOwnerHelpersIgnoreLimitedMembers
```

The limited member in the fixture is exactly what makes that visible. Note that `t.Fatalf` stops the
function at the first assertion, so the `AwaitingSignature` and `AgreementsLocked` checks below it never
run under this mutation — that is expected, not a second defect.

**(b) Runes, not bytes.** In `validateAgreementBody`, change
`utf8.RuneCountInString(trimmed) > MaxAgreementBodyLen` to `len(trimmed) > MaxAgreementBodyLen`. The
fixture body is 500 Chinese runes, which is 1500 bytes:

```
--- FAIL: TestAgreementProposalValidate/a_body_at_the_rune_cap
    agreement_test.go:NN: Validate() = an agreement body is too long, want <nil>
--- FAIL: TestAgreementProposalValidate/a_body_trimmed_back_under_the_cap
    agreement_test.go:NN: Validate() = an agreement body is too long, want <nil>
```

Exactly those two subtests, and the other twelve stay green — including `a body one rune over`, which
returns the same error either way and would have made this mutation invisible on its own.

**(c) Continuous numbering.** In `AgreementDisplayNumbers`, move `next := 1` from above the loop to the
first line inside it:

```
    agreement_test.go:NN: AgreementDisplayNumbers([2 0 3]) = [[1 2] [] [1 2 3]], want [[1 2] [] [3 4 5]]
--- FAIL: TestAgreementDisplayNumbersRunAcrossSections
```

The empty middle section is what makes the third section's start observable; with `[2, 3]` the mutation
would still produce a different answer, but the fixture also proves an empty section consumes no number.

- [ ] **Step 6: Lint and commit**

Run `make lint` first — `lint-arch` fails the build if `agreement.go` imported anything outside the
standard library, which is the one rule this file can break without any test noticing.

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
make lint
git add api/internal/domain/agreement.go api/internal/domain/agreement_test.go api/internal/domain/errors.go
git commit -m "feat(agreements): the rules that need no database, and fourteen sentinels

Step 2 was a build failure, not a test failure: 'undefined: domain.AgreementProposal'
and nine more, then 'FAIL ... [build failed]' with no test binary produced.

Three mutation checks afterwards, all test failures. Deleting the
'if m.Role == RoleOwner' guard in RequiredSigners reddened
TestOwnerHelpersIgnoreLimitedMembers on 'RequiredSigners = [m1 m9 m2], want
[m1 m2]'. Swapping utf8.RuneCountInString for len in validateAgreementBody
reddened TestAgreementProposalValidate/a_body_at_the_rune_cap and
/a_body_trimmed_back_under_the_cap on 'want <nil>', the other twelve subtests
staying green. Moving 'next := 1' inside AgreementDisplayNumbers' loop
reddened TestAgreementDisplayNumbersRunAcrossSections on
'[[1 2] [] [1 2 3]], want [[1 2] [] [3 4 5]]'.

MaxAgreementParkNoteLen is deliberately unasserted here: an alias of
MaxAgreementNoteLen is also 500, so only Task 4's Park test can separate them."
```

---

---

### Task 3: The port, the composed read, and the proposal view both sides share

**Files:**
- Modify: `api/internal/usecase/ports.go` — insert after `VisionRepository`'s closing brace (`ports.go:1393`), above `// ErrBrowseUnavailable` (`ports.go:1395`), so the Marriage ports stay together
- Modify: `api/internal/usecase/testdouble_test.go` — append at the end of the file. Its import block already carries `context`, `fmt`, `sort`, `time`, `internal/domain` and `internal/usecase`; nothing new is needed
- Create: `api/internal/usecase/agreement.go`, `api/internal/usecase/agreement_test.go`

**Interfaces:**

- **Consumes (Task 2):** `domain.ParseAgreementProposalStatus(s string) (domain.AgreementProposalStatus, error)`, `domain.ProposalAccepted`, `(domain.AgreementProposalStatus).IsOpen() bool`, `domain.RequiredSigners(all []domain.Membership) []string`, `domain.AwaitingSignature(all []domain.Membership, signed []string) []string`, `domain.AgreementsLocked(all []domain.Membership) bool`, `domain.AgreementDisplayNumbers(sizes []int) [][]int`, `domain.MinAgreementOwners`, and the sentinels `domain.ErrNotFound`, `domain.ErrForbidden`, `domain.ErrAgreementChanged`, `domain.ErrAgreementNotOpen`, `domain.ErrAgreementSectionNameTaken`, `domain.ErrUnknownAgreementProposalKind`.
- **Consumes (already in the tree):** `membershipsFrom(views []MemberView) []domain.Membership` (`member.go:130`), `MembershipRepository.List(ctx, householdID) ([]MemberView, error)` (`ports.go:125`), `MemberView{Membership domain.Membership; User domain.User}` (`ports.go:119`).
- **Produces — Tasks 5 and 6 implement the port, Tasks 7 and 8 render these views.** Copy from the plan header's interface map; every name below is that map's, with one flagged addition:

```go
// ports.go
type AgreementSectionRecord struct{ ID, Name string; CreatedAt time.Time }
type AgreementRecord struct{ ID, SectionID, Body, AddedByProposalID string; CreatedAt time.Time }
type AgreementProposalRecord struct {
	ID, Kind, Status, SectionID, TargetAgreementID, Body, PreviousBody, Note, ParkNote,
	ProposedByMembershipID string
	CreatedAt              time.Time
	ResolvedAt             *time.Time
	SignedByMembershipIDs  []string
}
type AgreementDocument struct {
	Sections   []AgreementSectionRecord
	Agreements []AgreementRecord         // live only, removed_at IS NULL
	Open       []AgreementProposalRecord // pending and parked, created_at asc
	Accepted   []AgreementProposalRecord // accepted only, resolved_at asc
}
type AgreementProposalWrite struct {
	HouseholdID, Kind, SectionID, TargetAgreementID, Body, PreviousBody, Note,
	ProposedByMembershipID string
	CreatedAt              time.Time
}
type AgreementSignatureWrite struct {
	HouseholdID, ProposalID, MembershipID string
	At                                    time.Time
}
type AgreementRepository interface {
	Document(ctx context.Context, householdID string) (AgreementDocument, error)
	Proposal(ctx context.Context, householdID, proposalID string) (AgreementProposalRecord, error)
	CreateSection(ctx context.Context, householdID, name string, createdAt time.Time) (AgreementSectionRecord, error)
	CreateSections(ctx context.Context, householdID string, names []string, createdAt time.Time) ([]AgreementSectionRecord, error)
	CreateProposal(ctx context.Context, in AgreementProposalWrite) (AgreementProposalRecord, error)
	Sign(ctx context.Context, in AgreementSignatureWrite) (AgreementProposalRecord, error)
	Park(ctx context.Context, householdID, proposalID, note string, at time.Time) (AgreementProposalRecord, error)
	Withdraw(ctx context.Context, householdID, proposalID, byMembershipID string, at time.Time) (AgreementProposalRecord, error)
}

// agreement.go
type AgreementOwner struct{ MembershipID, Name string }
type AgreementLine struct{ ID, Body string; Number int }
type AgreementSectionView struct {
	ID, Name   string
	Count      int
	Visible    bool
	Agreements []AgreementLine
}
type AgreementProposalView struct {
	ID, Kind, Status, SectionID, SectionName, TargetAgreementID string
	Body, PreviousBody, Note, ParkNote                          string
	ProposedByMembershipID, ProposedByName                      string
	ProposedAt                                                  time.Time
	AwaitingNames                                               []string
	TargetChanged                                               bool
	SignedByMembershipIDs                                       []string
}
type AgreementHistoryEntry struct {
	Version                                  int
	ProposalID, Kind, SectionID, SectionName string
	Body, PreviousBody, Note, ProposedByName string
	SignedByNames                            []string
	AcceptedAt                               time.Time
}
type AgreementsView struct {
	Locked    bool
	Owners    []AgreementOwner        // its length IS the owner count; no second count travels beside it
	Version   int
	UpdatedAt *time.Time
	Sections  []AgreementSectionView
	Proposals []AgreementProposalView // pending and parked only
	History   []AgreementHistoryEntry // newest first
}

var ErrAgreementDocumentCorrupt error

func NewAgreementService(agreements AgreementRepository, members MembershipRepository) *AgreementService
func (s *AgreementService) Get(ctx context.Context, householdID string) (AgreementsView, error)
func (s *AgreementService) proposalView(
	p AgreementProposalRecord,
	all []domain.Membership,
	names map[string]string,        // membership id -> display name
	liveBody map[string]string,     // live agreement id -> its current body
	sectionNames map[string]string, // section id -> name
) (AgreementProposalView, error)
```

**Two notes on that block, because a later task will otherwise think the header was ignored.**

1. `AgreementProposalView.SignedByMembershipIDs` is the **one field not in the plan header's interface map**, and it is here because Task 7's DTO mapper cannot compute `canAgree` without it. The spec fixes `canAgree = !locked && (!signedByViewer || len(awaitingNames) == 0)`, the viewer is a membership id the HTTP layer holds, and nothing else on the view or on `AgreementsView` says who has already signed — `AwaitingNames` is names, and `all []domain.Membership` never reaches the mapper. It carries the record's slice unchanged and **never goes on the wire**: `agreementProposalDTO` has no such field.
2. `Get` is implemented as `compose` minus its lookups (Step 5). `proposalView` needs four maps `AgreementsView` does not carry, so a write cannot get them from `Get`'s return value; `compose(ctx, householdID) (AgreementsView, agreementLookups, error)` hands back both and `Get` drops the second. **A write reading through `compose` is reading through `Get`** — there is one walk and two entry points — so do not expect to find `s.Get(` inside a write body in Task 4.

---

- [ ] **Step 1: Write the ports** — into `ports.go` at the point named above. The record types are copied verbatim from the header's interface map; do not rename a field. These doc comments are what Tasks 5 and 6 implement against, and the only description of the contract they get.

```go
// AgreementSectionRecord is one stored section: a label, not a promise --
// creating one is immediate and unsigned (decision 8).
type AgreementSectionRecord struct {
	ID, Name  string
	CreatedAt time.Time
}

// AgreementRecord is one LIVE agreement. A removed one never appears here:
// removal is a stamp, not a delete (decision 9), and removed_at IS NULL
// belongs in SQL, never in a caller.
type AgreementRecord struct {
	ID, SectionID, Body, AddedByProposalID string
	CreatedAt                              time.Time
}

// AgreementProposalRecord is one proposed change as stored. Kind and Status
// hold the column re-parsed, never cast. ResolvedAt is a pointer because
// "still open" is a state and the zero time is not a moment.
// SignedByMembershipIDs may hold a signature from someone who is no longer an
// owner: a true record that stops counting (decision 4).
type AgreementProposalRecord struct {
	ID, Kind, Status, SectionID, TargetAgreementID, Body, PreviousBody, Note, ParkNote,
	ProposedByMembershipID string
	CreatedAt              time.Time
	ResolvedAt             *time.Time
	SignedByMembershipIDs  []string
}

// AgreementDocument is the whole screen in one read. Open and Accepted are
// two slices because they are in two orders and one cannot hold both;
// withdrawn proposals appear in neither, excluded in SQL. Every slice
// non-nil, so a caller ranges over it without a check.
type AgreementDocument struct {
	Sections   []AgreementSectionRecord
	Agreements []AgreementRecord         // live only, removed_at IS NULL
	Open       []AgreementProposalRecord // pending and parked, created_at asc
	Accepted   []AgreementProposalRecord // accepted only, resolved_at asc
}

// AgreementProposalWrite is one propose. HouseholdID and
// ProposedByMembershipID are stamped by the service from the route and the
// session, never read from a request body. SectionID is set only for an add:
// on an edit or a remove the repository copies the target's own section, so
// there is nothing here for a caller to get wrong.
type AgreementProposalWrite struct {
	HouseholdID, Kind, SectionID, TargetAgreementID, Body, PreviousBody, Note,
	ProposedByMembershipID string
	CreatedAt              time.Time
}

// AgreementSignatureWrite is one Agree. MembershipID is STORED, never
// consulted for permission -- PaymentWrite.PaidByMembershipID's contract.
type AgreementSignatureWrite struct {
	HouseholdID, ProposalID, MembershipID string
	At                                    time.Time
}

// AgreementRepository stores one household's agreements, the sections that
// group them, and the append-only log of every change ever proposed. Every
// method filters on householdID in SQL: another household's row must be
// indistinguishable from one that does not exist. Ordering is created_at, id
// everywhere except AgreementDocument.Accepted, which is resolved_at, id --
// a version is the k-th ACCEPTANCE, and two proposals created A then B can be
// accepted B then A. Nothing here reads a clock.
type AgreementRepository interface {
	// Document is the whole screen in one read, kind and status re-parsed and
	// never cast. Unbounded on purpose: a household writes a few agreements a
	// year, and the version, the history list and the Retros page's
	// To-discuss block all derive from more than one of its four slices.
	Document(ctx context.Context, householdID string) (AgreementDocument, error)
	// Proposal returns one proposal whatever its status, withdrawn included,
	// so the withdraw handler's proposer check costs one query rather than a
	// composed document. domain.ErrNotFound for an unknown or foreign id.
	Proposal(ctx context.Context, householdID, proposalID string) (AgreementProposalRecord, error)
	// CreateSection adds one label. A clash with UNIQUE (household_id, name)
	// is domain.ErrAgreementSectionNameTaken, mapped by CONSTRAINT NAME above
	// the generic 23505 case (decision 19), or the screen cannot tell "you
	// already have that section" from anything else that could collide.
	CreateSection(ctx context.Context, householdID, name string, createdAt time.Time) (AgreementSectionRecord, error)
	// CreateSections is "Use starter set" (decision 17): every name in ONE
	// transaction, ON CONFLICT DO NOTHING so a second click is a no-op rather
	// than a 409, then read back inside it. Two of four landing would leave a
	// household half-seeded with no button left to ask for the rest. The
	// read-back is how the caller proves all four landed; nothing renders
	// from its order, since the route answers with the whole freshly composed
	// document and render order is always the document's.
	CreateSections(ctx context.Context, householdID string, names []string, createdAt time.Time) ([]AgreementSectionRecord, error)
	// CreateProposal writes the proposal row AND the proposer's implicit
	// signature (decision 5), verifying the target first for an edit or a
	// remove, all in ONE transaction: either all of it happens or none of it
	// does. A proposal without its proposer's signature would ask both owners
	// to be the second signer of a set of one, and no route here could repair
	// it -- the failure InviteRepository.Accept's own comment describes. The
	// target must exist in this household, still be live, and its body must
	// equal PreviousBody exactly, compared as stored and never re-trimmed;
	// either failure is domain.ErrAgreementChanged with nothing written. The
	// target's section_id is copied onto the proposal in the same statement,
	// which is why domain.AgreementProposal.Validate refuses a
	// caller-supplied one on an edit or a remove. Zero rows on the proposer
	// lookup through memberships (this household, role = 'owner') rolls it
	// all back with domain.ErrForbidden.
	CreateProposal(ctx context.Context, in AgreementProposalWrite) (AgreementProposalRecord, error)
	// Sign records one Agree and, when that completes the signing set,
	// applies the change -- all in ONE transaction, on that transaction's OWN
	// connection: either all of it happens or none of it does. A pool-backed
	// call inside pgx.BeginFunc takes a second connection while the first is
	// held, and enough concurrent signers deadlock against MaxConns -- the
	// hang VisionRepo.Save shipped. It is the only method that writes an
	// agreements row. The set is every CURRENT owner, counted in this
	// transaction (decision 4), and the lock that matters is on the TARGET
	// agreement, not the proposal (decision 12); that target check runs
	// BEFORE the signature lands, or a middle signer's agreement is recorded
	// against wording that has already moved. Applying is a switch on kind
	// with a refusing default: an add inserts, a remove stamps, an edit does
	// both -- so an edit CHANGES the agreement's id, and the new row sorts
	// last in its section exactly as created_at, id puts it.
	// domain.ErrNotFound for an unknown id, domain.ErrAgreementNotOpen for a
	// resolved one, domain.ErrAgreementChanged when the target moved,
	// domain.ErrForbidden when the signer is not an owner here.
	Sign(ctx context.Context, in AgreementSignatureWrite) (AgreementProposalRecord, error)
	// Park is Discuss: the proposal stays OPEN and moves to the Retros page's
	// To-discuss block (decision 7); parking twice replaces the note. One
	// guarded UPDATE with the status condition in the WHERE clause, never a
	// service if, because a check-then-write races; zero rows is diagnosed by
	// one re-read -- gone is domain.ErrNotFound, resolved is
	// domain.ErrAgreementNotOpen, the re-read's own failure passes through
	// untouched. NOTHING here touches a retro table and there is no foreign
	// key to a retro row: the next retro usually does not exist yet, which is
	// exactly when a couple parks something.
	Park(ctx context.Context, householdID, proposalID, note string, at time.Time) (AgreementProposalRecord, error)
	// Withdraw is the same guarded UPDATE plus AND (proposed_by_membership_id
	// = $by OR that membership is no longer an owner here): proposer-only
	// until the proposer leaves, then any owner (decision 15). That clause is
	// a BACKSTOP -- the handler decides and answers first, and this method
	// never branches on $by. Four diagnose legs, in order: gone is
	// domain.ErrNotFound; resolved is domain.ErrAgreementNotOpen; open,
	// someone else's, and that someone still an owner is domain.ErrForbidden;
	// the re-read's own failure is folded into none of the other three.
	Withdraw(ctx context.Context, householdID, proposalID, byMembershipID string, at time.Time) (AgreementProposalRecord, error)
}
```

- [ ] **Step 2: Write the failing service tests** — create `api/internal/usecase/agreement_test.go`. The first fixture carries all four statuses, two filled sections and an empty one, because that is the only shape in which a wrong version, a wrong `Proposals` slice and a wrong numbering are all visible at once. It carries **three** accepted proposals, not two, because the spec's `UpdatedAt` case is "the newest acceptance's timestamp with three".

```go
package usecase_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// seedMembers builds the household every agreement test runs against: the
// given number of owners named in that order, plus a limited member who can
// never be asked to sign (domain.ValidateMembershipChange refuses CapMarriage
// to one). Without that member the signing-set helpers get tested minus their
// filter.
func seedMembers(t *testing.T, owners int) *membershipDouble {
	t.Helper()
	members := newMembershipDouble(newUserDouble())
	add := func(id, name string, role domain.Role) {
		members.users.put(usecase.StoredUser{User: domain.User{ID: "u-" + id, DisplayName: name}})
		members.put(domain.Membership{ID: id, HouseholdID: "hh", UserID: "u-" + id, Role: role})
	}
	for i, name := range []string{"Andreas", "Christine", "Priya"}[:owners] {
		add(fmt.Sprintf("m%d", i+1), name, domain.RoleOwner)
	}
	add("kid", "Kiddo", domain.RoleLimited)
	return members
}

func TestAgreementGetComposesTheWholeDocument(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money, home := repo.seedSection("Money"), repo.seedSection("Home & kids")
	repo.seedSection("Us") // no live agreement: invisible, but the picker still offers it
	repo.seedAgreement(money.ID, "Anything over $200 gets a conversation")
	repo.seedAgreement(money.ID, "We review the budget on the first Sunday")
	repo.seedAgreement(home.ID, "Phone-free dinners")
	june := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	august := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	september := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	repo.seedProposal("accepted", "add", money.ID, "Anything over $200 gets a conversation", &june, "m1", "m2")
	repo.seedProposal("accepted", "add", money.ID, "We review the budget on the first Sunday", &august, "m1", "m2")
	repo.seedProposal("accepted", "add", home.ID, "Phone-free dinners", &september, "m1", "m2")
	repo.seedProposal("pending", "add", home.ID, "One night a week is ours", nil, "m1")
	repo.seedProposal("parked", "add", money.ID, "We split the holiday fund", nil, "m1")
	repo.seedProposal("withdrawn", "add", money.ID, "Never mind", &september, "m1")

	view, err := usecase.NewAgreementService(repo, members).Get(context.Background(), "hh")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// count(ACCEPTED) + 1 (decision 10): the pending, parked and withdrawn
	// rows count toward nothing.
	if view.Version != 4 {
		t.Errorf("Version = %d, want 4 -- count(accepted) + 1, not count(*)", view.Version)
	}
	if view.UpdatedAt == nil || !view.UpdatedAt.Equal(september) {
		t.Errorf("UpdatedAt = %v, want the newest acceptance %v", view.UpdatedAt, september)
	}
	if len(view.Proposals) != 2 {
		t.Errorf("len(Proposals) = %d, want 2 -- pending and parked only", len(view.Proposals))
	}
	// Numbering runs continuously ACROSS sections: Money 01-02, Home 03.
	if got := view.Sections[1].Agreements[0].Number; got != 3 {
		t.Errorf("Home's first agreement is number %d, want 3 -- numbering does not restart", got)
	}
	if view.Sections[0].Count != 2 || !view.Sections[0].Visible {
		t.Errorf("Money = {Count %d, Visible %v}, want {2, true}", view.Sections[0].Count, view.Sections[0].Visible)
	}
	// An empty section travels with Visible false rather than being dropped:
	// the page hides it, the propose picker still offers it (decision 8).
	if view.Sections[2].Visible || view.Sections[2].Count != 0 {
		t.Errorf("Us = {Count %d, Visible %v}, want {0, false} and still present",
			view.Sections[2].Count, view.Sections[2].Visible)
	}
	// Every open proposal carries its section's NAME, not only its id: the
	// card prints it and the write responses reuse the same composition.
	if view.Proposals[0].SectionName != "Home & kids" {
		t.Errorf("the pending proposal's SectionName = %q, want %q", view.Proposals[0].SectionName, "Home & kids")
	}
	// An add has no target, so it can never be stale (decision 14).
	if view.Proposals[0].TargetChanged {
		t.Error("TargetChanged = true on an add -- an add has no target to go stale")
	}
	// History is the accepted slice reversed, newest first, each numbered
	// with the version that change PRODUCED.
	if len(view.History) != 3 || view.History[0].Version != 4 || view.History[2].Version != 2 {
		t.Fatalf("history versions = %+v, want v4, v3, v2", view.History)
	}
	if got := view.History[0].SignedByNames; len(got) != 2 || got[0] != "Andreas" {
		t.Errorf("SignedByNames = %v, want both owners", got)
	}
	if view.History[0].SectionName != "Home & kids" {
		t.Errorf("history[0].SectionName = %q, want %q", view.History[0].SectionName, "Home & kids")
	}
}

// A household down to one owner keeps seeing everything it agreed to and its
// frozen proposals (decision 3): Locked gates writes, it never hides rows.
func TestAgreementGetIsLockedForOneOwnerAndStillCarriesTheDocument(t *testing.T) {
	members := seedMembers(t, 1)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money := repo.seedSection("Money")
	repo.seedAgreement(money.ID, "Anything over $200 gets a conversation")
	repo.seedProposal("pending", "add", money.ID, "We split the holiday fund", nil, "m1")

	view, err := usecase.NewAgreementService(repo, members).Get(context.Background(), "hh")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !view.Locked {
		t.Fatal("Locked = false on a one-owner household")
	}
	if len(view.Owners) != 1 || len(view.Sections[0].Agreements) != 1 || len(view.Proposals) != 1 {
		t.Fatalf("the locked document lost rows: %d owners, %d agreements, %d proposals",
			len(view.Owners), len(view.Sections[0].Agreements), len(view.Proposals))
	}
}

// UpdatedAt is nil before anything is accepted, so the header can say nothing
// about when the document last moved rather than claiming it moved at the
// zero time. Neither a pending nor a withdrawn proposal is an acceptance.
func TestAgreementGetHasNoUpdatedAtUntilSomethingIsAccepted(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money := repo.seedSection("Money")
	withdrawnAt := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	repo.seedProposal("pending", "add", money.ID, "We split the holiday fund", nil, "m1")
	repo.seedProposal("withdrawn", "add", money.ID, "Never mind", &withdrawnAt, "m1")

	view, err := usecase.NewAgreementService(repo, members).Get(context.Background(), "hh")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.UpdatedAt != nil {
		t.Errorf("UpdatedAt = %v, want nil -- nothing has been accepted", view.UpdatedAt)
	}
	if view.Version != 1 || len(view.History) != 0 {
		t.Errorf("v%d with %d history entries, want v1 and none", view.Version, len(view.History))
	}
}
```

- [ ] **Step 3: Run them and watch them fail** — `cd api && go test ./internal/usecase/ -run TestAgreement -count=1 -v`. Expected, roughly:

```
# github.com/andreasoentoro/hearth/api/internal/usecase_test [build failed]
internal/usecase/agreement_test.go:41:10: undefined: newAgreementRepoDouble
internal/usecase/agreement_test.go:61:11: undefined: usecase.NewAgreementService
FAIL	github.com/andreasoentoro/hearth/api/internal/usecase [build failed]
```

That is a **build** failure, not a test failure — both print the word `FAIL`. Note which you saw; the commit body in Step 8 asks for it.

- [ ] **Step 4: Write the in-memory double** — append to `api/internal/usecase/testdouble_test.go`, under a `// --- AgreementRepository ---` banner matching the file's existing section comments. This is the whole of it; Task 4 adds nothing to this file.

```go
// --- AgreementRepository ---------------------------------------------

// agreementRepoDouble is the in-memory AgreementRepository every
// AgreementService test runs against. It implements EVERY port method,
// including ones no test calls (Liskov, CLAUDE.md), and it honours the
// refusals the real one makes: a caller must never need to know which
// implementation it is holding. writes counts only calls that actually
// changed a row, so "refused" is distinguishable from "refused eventually" --
// the counter is the whole point of the one-owner test.
//
// Wire setMembers, or Sign can never complete a set: completion is every
// CURRENT owner having signed, which the real transaction counts through
// memberships in-transaction.
type agreementRepoDouble struct {
	sections   []usecase.AgreementSectionRecord
	agreements []usecase.AgreementRecord
	// removed is the removed_at stamp (decision 9): the row stays and the
	// read filters. It lives in a side map because AgreementRecord is the
	// LIVE shape and carries no removed_at -- which is the port's contract,
	// not an omission.
	removed   map[string]time.Time
	proposals []*usecase.AgreementProposalRecord
	members   *membershipDouble
	writes    int
	// lastWithdrawBy is the byMembershipID the service last handed over, set
	// before any refusal. The service must never branch on that id (decision
	// 15: the proposer check is the handler's), and this is the only way a
	// test can see whether it did -- Task 4's third mutation check reddens
	// here.
	lastWithdrawBy string
	n              int
}

var agreementSeedAt = time.Date(2026, 7, 1, 9, 0, 0, 0, time.UTC)

func newAgreementRepoDouble() *agreementRepoDouble {
	return &agreementRepoDouble{removed: map[string]time.Time{}}
}

func (d *agreementRepoDouble) setMembers(m *membershipDouble) { d.members = m }
func (d *agreementRepoDouble) id(kind string) string          { d.n++; return fmt.Sprintf("%s-%d", kind, d.n) }

func (d *agreementRepoDouble) seedSection(name string) usecase.AgreementSectionRecord {
	s := usecase.AgreementSectionRecord{ID: d.id("section"), Name: name, CreatedAt: agreementSeedAt}
	d.sections = append(d.sections, s)
	return s
}

func (d *agreementRepoDouble) seedAgreement(sectionID, body string) usecase.AgreementRecord {
	a := usecase.AgreementRecord{ID: d.id("agreement"), SectionID: sectionID, Body: body,
		AddedByProposalID: "seeded", CreatedAt: agreementSeedAt}
	d.agreements = append(d.agreements, a)
	return a
}

// seedProposal writes one row at any of the four statuses, always proposed by
// m1: the version test needs all four, since count(accepted) and count(*)
// agree on anything less. Edits and removes are made through Propose in the
// tests that need them, so they arrive with the section the target gave them.
func (d *agreementRepoDouble) seedProposal(status, kind, sectionID, body string,
	resolvedAt *time.Time, signers ...string) *usecase.AgreementProposalRecord {
	p := &usecase.AgreementProposalRecord{ID: d.id("proposal"), Kind: kind, Status: status,
		SectionID: sectionID, Body: body, ProposedByMembershipID: "m1", CreatedAt: agreementSeedAt,
		ResolvedAt: resolvedAt, SignedByMembershipIDs: signers}
	d.proposals = append(d.proposals, p)
	return p
}

func (d *agreementRepoDouble) Document(_ context.Context, _ string) (usecase.AgreementDocument, error) {
	doc := usecase.AgreementDocument{
		Sections:   append([]usecase.AgreementSectionRecord{}, d.sections...),
		Agreements: make([]usecase.AgreementRecord, 0, len(d.agreements)),
		Open:       []usecase.AgreementProposalRecord{},
		Accepted:   []usecase.AgreementProposalRecord{},
	}
	for _, a := range d.agreements {
		if _, gone := d.removed[a.ID]; gone {
			continue // removed_at IS NULL, which belongs in SQL and never in a caller
		}
		doc.Agreements = append(doc.Agreements, a)
	}
	for _, p := range d.proposals {
		switch p.Status {
		case "pending", "parked":
			doc.Open = append(doc.Open, *p)
		case "accepted":
			doc.Accepted = append(doc.Accepted, *p)
		}
		// withdrawn lands in neither slice, excluded in SQL by the real one.
	}
	// Accepted is resolved_at asc, which is NOT creation order: two proposals
	// created A then B can be accepted B then A, and the version a change
	// produced is its position in THIS order. Open keeps insertion order,
	// which is created_at asc because every seeded row shares a timestamp.
	sort.SliceStable(doc.Accepted, func(i, j int) bool {
		l, r := doc.Accepted[i].ResolvedAt, doc.Accepted[j].ResolvedAt
		if l == nil || r == nil {
			return false
		}
		return l.Before(*r)
	})
	return doc, nil
}

func (d *agreementRepoDouble) Proposal(_ context.Context, _, proposalID string) (usecase.AgreementProposalRecord, error) {
	for _, p := range d.proposals {
		if p.ID == proposalID {
			return *p, nil
		}
	}
	return usecase.AgreementProposalRecord{}, domain.ErrNotFound
}

func (d *agreementRepoDouble) CreateSection(_ context.Context, _, name string,
	createdAt time.Time) (usecase.AgreementSectionRecord, error) {
	for _, s := range d.sections {
		if s.Name == name {
			return usecase.AgreementSectionRecord{}, domain.ErrAgreementSectionNameTaken
		}
	}
	d.writes++
	s := usecase.AgreementSectionRecord{ID: d.id("section"), Name: name, CreatedAt: createdAt}
	d.sections = append(d.sections, s)
	return s, nil
}

// CreateSections is ON CONFLICT DO NOTHING then a read-back: a name already
// there is skipped without a write, and every name asked for comes back,
// which is how the caller proves all four landed.
func (d *agreementRepoDouble) CreateSections(ctx context.Context, householdID string, names []string,
	createdAt time.Time) ([]usecase.AgreementSectionRecord, error) {
	out := make([]usecase.AgreementSectionRecord, 0, len(names))
	for _, name := range names {
		s, err := d.CreateSection(ctx, householdID, name, createdAt)
		if err != nil {
			for _, existing := range d.sections {
				if existing.Name == name {
					s = existing
				}
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// CreateProposal writes the proposal row AND the proposer's implicit
// signature (decision 5) as one unit, so writes moves by one and not two. For
// an edit or a remove it verifies the target BEFORE counting a write and
// copies the target's section onto the proposal -- which is what the real
// statement does, and what makes Validate's refusal of a caller-supplied
// section safe rather than lossy.
func (d *agreementRepoDouble) CreateProposal(_ context.Context,
	in usecase.AgreementProposalWrite) (usecase.AgreementProposalRecord, error) {
	sectionID := in.SectionID
	if in.Kind != "add" {
		target, err := d.liveAgreement(in.TargetAgreementID)
		if err != nil {
			return usecase.AgreementProposalRecord{}, err
		}
		if target.Body != in.PreviousBody {
			return usecase.AgreementProposalRecord{}, domain.ErrAgreementChanged
		}
		sectionID = target.SectionID
	}
	d.writes++
	p := &usecase.AgreementProposalRecord{ID: d.id("proposal"), Kind: in.Kind, Status: "pending",
		SectionID: sectionID, TargetAgreementID: in.TargetAgreementID, Body: in.Body,
		PreviousBody: in.PreviousBody, Note: in.Note, ProposedByMembershipID: in.ProposedByMembershipID,
		CreatedAt: in.CreatedAt, SignedByMembershipIDs: []string{in.ProposedByMembershipID}}
	d.proposals = append(d.proposals, p)
	return *p, nil
}

// Sign completes only when every CURRENT owner has signed and there are at
// least MinAgreementOwners of them -- what the real transaction counts inside
// itself. The target check is step 2 and runs before the signature of step 3,
// because after step 4 a middle signer's agreement is recorded against
// wording that has already moved.
func (d *agreementRepoDouble) Sign(ctx context.Context,
	in usecase.AgreementSignatureWrite) (usecase.AgreementProposalRecord, error) {
	p, err := d.open(in.ProposalID)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	var target usecase.AgreementRecord
	if p.Kind != "add" {
		target, err = d.liveAgreement(p.TargetAgreementID)
		if err != nil {
			return usecase.AgreementProposalRecord{}, err
		}
		if target.Body != p.PreviousBody {
			return usecase.AgreementProposalRecord{}, domain.ErrAgreementChanged
		}
	}
	// Step 3's INSERT selects the signer through memberships (this household,
	// role = 'owner'); zero rows there is ErrForbidden, the backstop behind
	// requireOwner. No test in Tasks 3 and 4 needs it, and it is here anyway,
	// because a double that skips a refusal its port documents is a double a
	// caller can pass and the real thing cannot.
	signerIsOwner, err := d.isOwner(ctx, in.HouseholdID, in.MembershipID)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	if !signerIsOwner {
		return usecase.AgreementProposalRecord{}, domain.ErrForbidden
	}
	d.writes++
	if !signedBy(p.SignedByMembershipIDs, in.MembershipID) {
		p.SignedByMembershipIDs = append(p.SignedByMembershipIDs, in.MembershipID)
	}
	views, err := d.members.List(ctx, in.HouseholdID)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	var owners, signed int
	for _, v := range views {
		if v.Membership.Role != domain.RoleOwner {
			continue
		}
		owners++
		if signedBy(p.SignedByMembershipIDs, v.Membership.ID) {
			signed++
		}
	}
	if owners < domain.MinAgreementOwners || signed != owners {
		return *p, nil // not complete: commit here, status unchanged
	}
	// Step 5, in a switch with a refusing default. An edit is a remove and an
	// add together, so the agreement's ID CHANGES and the new row sorts last
	// in its section -- exactly where created_at, id puts it in the real one.
	switch p.Kind {
	case "add":
		d.addAgreement(p, in.At)
	case "edit":
		d.removed[target.ID] = in.At
		d.addAgreement(p, in.At)
	case "remove":
		d.removed[target.ID] = in.At
	default:
		return usecase.AgreementProposalRecord{}, domain.ErrUnknownAgreementProposalKind
	}
	at := in.At
	p.Status, p.ResolvedAt = "accepted", &at
	return *p, nil
}

func (d *agreementRepoDouble) addAgreement(p *usecase.AgreementProposalRecord, at time.Time) {
	d.agreements = append(d.agreements, usecase.AgreementRecord{ID: d.id("agreement"),
		SectionID: p.SectionID, Body: p.Body, AddedByProposalID: p.ID, CreatedAt: at})
}

func (d *agreementRepoDouble) Park(_ context.Context, _, proposalID, note string,
	_ time.Time) (usecase.AgreementProposalRecord, error) {
	p, err := d.open(proposalID)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	d.writes++
	p.Status, p.ParkNote = "parked", note
	return *p, nil
}

// Withdraw runs the port's four legs in order: gone, resolved, then the
// WHERE clause's backstop -- proposer-only until the proposer is no longer an
// owner here (decision 15). The backstop is here rather than in the service
// because a caller must never need to know which implementation it holds, and
// because it is what lets a test prove the SERVICE was not the thing that
// decided.
func (d *agreementRepoDouble) Withdraw(ctx context.Context, householdID, proposalID,
	byMembershipID string, at time.Time) (usecase.AgreementProposalRecord, error) {
	d.lastWithdrawBy = byMembershipID
	p, err := d.open(proposalID)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	if byMembershipID != p.ProposedByMembershipID {
		stillOwner, err := d.isOwner(ctx, householdID, p.ProposedByMembershipID)
		if err != nil {
			return usecase.AgreementProposalRecord{}, err
		}
		if stillOwner {
			return usecase.AgreementProposalRecord{}, domain.ErrForbidden
		}
	}
	d.writes++
	stamp := at
	p.Status, p.ResolvedAt = "withdrawn", &stamp
	return *p, nil
}

// open is the guarded UPDATE's WHERE clause and its first two diagnose legs:
// gone is ErrNotFound, resolved is ErrAgreementNotOpen, and NEITHER
// increments writes -- or a refusal is indistinguishable from a write.
func (d *agreementRepoDouble) open(proposalID string) (*usecase.AgreementProposalRecord, error) {
	for _, p := range d.proposals {
		if p.ID != proposalID {
			continue
		}
		status, err := domain.ParseAgreementProposalStatus(p.Status)
		if err != nil {
			return nil, err
		}
		if !status.IsOpen() {
			return nil, domain.ErrAgreementNotOpen
		}
		return p, nil
	}
	return nil, domain.ErrNotFound
}

// liveAgreement is step 2's SELECT ... FOR UPDATE minus the lock. A target
// that is gone, removed or reworded is ErrAgreementChanged and never
// ErrNotFound: "it vanished" and "someone changed it" are the same answer to
// the caller, and a different answer from "no such proposal".
func (d *agreementRepoDouble) liveAgreement(id string) (usecase.AgreementRecord, error) {
	for _, a := range d.agreements {
		if a.ID != id {
			continue
		}
		if _, gone := d.removed[a.ID]; gone {
			return usecase.AgreementRecord{}, domain.ErrAgreementChanged
		}
		return a, nil
	}
	return usecase.AgreementRecord{}, domain.ErrAgreementChanged
}

func (d *agreementRepoDouble) isOwner(ctx context.Context, householdID, membershipID string) (bool, error) {
	views, err := d.members.List(ctx, householdID)
	if err != nil {
		return false, err
	}
	for _, v := range views {
		if v.Membership.ID == membershipID && v.Membership.Role == domain.RoleOwner {
			return true, nil
		}
	}
	return false, nil
}

func signedBy(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

var _ usecase.AgreementRepository = (*agreementRepoDouble)(nil)
```

- [ ] **Step 5: Write the composed read** — create `api/internal/usecase/agreement.go`. Imports: `context`, `errors`, `fmt`, `time`, and `github.com/andreasoentoro/hearth/api/internal/domain`. Start with the seven view types exactly as the Interfaces block declares them, then:

```go
// AgreementService composes the Agreements screen and is its only write path.
// Two ports and no clock: every write takes at time.Time, so the wall clock is
// read once, at the HTTP layer. No method takes an actor parameter to decide
// whether a caller may act -- middleware enforces who is asking. There is
// deliberately no owner-count port: every pending card names who has still to
// sign, so the names are needed anyway and the count is that list's length --
// the locked screen and the pending card cannot disagree.
type AgreementService struct {
	agreements AgreementRepository
	members    MembershipRepository
}

func NewAgreementService(agreements AgreementRepository, members MembershipRepository) *AgreementService {
	return &AgreementService{agreements: agreements, members: members}
}

// ErrAgreementDocumentCorrupt is this layer refusing to render a document
// holding a row it never wrote: a section id naming no section, a proposal in
// the wrong slice, a history entry with no version. Deliberately unmapped in
// MapDomainError -- a logged 500 beats an agreement silently missing from the
// page or a history entry numbered v0. Declared here for the reason
// ErrSessionRevocationFailed is declared in member.go: it is this service's
// own vocabulary, not a domain rule.
var ErrAgreementDocumentCorrupt = errors.New("agreement document holds a row this code never wrote")

// agreementLookups are the four things proposalView needs and AgreementsView
// does not carry. compose hands them back so a write can run the row it just
// touched through exactly the code the document's own proposals went through:
// one composition, two entry points, and no way for the row and the document
// in a single response to describe the household differently.
type agreementLookups struct {
	all          []domain.Membership
	names        map[string]string // membership id -> display name
	liveBody     map[string]string // live agreement id -> its current body
	sectionNames map[string]string // section id -> name
}

// Get composes the whole screen in one walk -- the only place any of these
// figures is derived. The version and the 01..N numbering are computed here
// and stored nowhere (decisions 10 and 11).
func (s *AgreementService) Get(ctx context.Context, householdID string) (AgreementsView, error) {
	view, _, err := s.compose(ctx, householdID)
	return view, err
}

// compose is Get plus the lookups. Get is this minus its second return value,
// so a write reading through compose IS reading back through Get: there is
// one walk over the document and the two entry points share it.
func (s *AgreementService) compose(ctx context.Context, householdID string) (AgreementsView, agreementLookups, error) {
	views, err := s.members.List(ctx, householdID)
	if err != nil {
		return AgreementsView{}, agreementLookups{}, err
	}
	doc, err := s.agreements.Document(ctx, householdID)
	if err != nil {
		return AgreementsView{}, agreementLookups{}, err
	}
	lk := agreementLookups{
		all:          membershipsFrom(views), // member.go:130
		names:        make(map[string]string, len(views)),
		liveBody:     make(map[string]string, len(doc.Agreements)),
		sectionNames: make(map[string]string, len(doc.Sections)),
	}
	for _, v := range views {
		lk.names[v.Membership.ID] = v.User.DisplayName
	}
	owners := make([]AgreementOwner, 0, len(lk.all))
	for _, id := range domain.RequiredSigners(lk.all) {
		owners = append(owners, AgreementOwner{MembershipID: id, Name: lk.names[id]})
	}

	// Sections keep Document's order, each collecting its live agreements and
	// its Count in the SAME walk that fills them, so a total and its own
	// breakdown cannot diverge -- the BudgetService.Month defect.
	index := make(map[string]int, len(doc.Sections))
	for i, sec := range doc.Sections {
		index[sec.ID] = i
		lk.sectionNames[sec.ID] = sec.Name
	}
	grouped := make([][]AgreementRecord, len(doc.Sections))
	for _, a := range doc.Agreements {
		i, ok := index[a.SectionID]
		if !ok {
			return AgreementsView{}, agreementLookups{}, fmt.Errorf("%w: agreement %s names section %s",
				ErrAgreementDocumentCorrupt, a.ID, a.SectionID)
		}
		grouped[i] = append(grouped[i], a)
		lk.liveBody[a.ID] = a.Body
	}
	sizes := make([]int, len(grouped))
	for i, rows := range grouped {
		sizes[i] = len(rows)
	}
	numbers := domain.AgreementDisplayNumbers(sizes) // one call: numbering runs across sections
	sections := make([]AgreementSectionView, 0, len(doc.Sections))
	for i, sec := range doc.Sections {
		lines := make([]AgreementLine, 0, len(grouped[i]))
		for j, a := range grouped[i] {
			lines = append(lines, AgreementLine{ID: a.ID, Body: a.Body, Number: numbers[i][j]})
		}
		// Visible is stamped here so the screen reads a flag rather than
		// re-deriving decision 8: the page renders the visible ones, the
		// picker offers them all, off one array.
		sections = append(sections, AgreementSectionView{ID: sec.ID, Name: sec.Name,
			Count: len(lines), Visible: len(lines) > 0, Agreements: lines})
	}

	proposals := make([]AgreementProposalView, 0, len(doc.Open))
	for _, p := range doc.Open {
		status, err := domain.ParseAgreementProposalStatus(p.Status)
		if err != nil {
			return AgreementsView{}, agreementLookups{}, err // a value no migration allowed: a corrupt row, not a request
		}
		if !status.IsOpen() {
			return AgreementsView{}, agreementLookups{}, fmt.Errorf("%w: proposal %s is %s in the open slice",
				ErrAgreementDocumentCorrupt, p.ID, p.Status)
		}
		view, err := s.proposalView(p, lk.all, lk.names, lk.liveBody, lk.sectionNames)
		if err != nil {
			return AgreementsView{}, agreementLookups{}, err
		}
		proposals = append(proposals, view)
	}

	// versionOf exists for exactly one consumer: each history entry's own
	// Version. No agreement carries an "added in v4" badge, because the design
	// draws none and a number nothing renders is a field that rots.
	versionOf := make(map[string]int, len(doc.Accepted))
	var updatedAt *time.Time
	for i, p := range doc.Accepted {
		status, err := domain.ParseAgreementProposalStatus(p.Status)
		if err != nil {
			return AgreementsView{}, agreementLookups{}, err
		}
		if status != domain.ProposalAccepted || p.ResolvedAt == nil {
			return AgreementsView{}, agreementLookups{}, fmt.Errorf("%w: proposal %s is %s in the accepted slice",
				ErrAgreementDocumentCorrupt, p.ID, p.Status)
		}
		versionOf[p.ID] = i + 2                // a document is v1 before anything is agreed
		updatedAt = doc.Accepted[i].ResolvedAt // resolved_at asc, so the last one wins
	}
	history := make([]AgreementHistoryEntry, 0, len(doc.Accepted))
	for i := len(doc.Accepted) - 1; i >= 0; i-- { // reversed, never re-sorted by a second rule
		p := doc.Accepted[i]
		version, ok := versionOf[p.ID]
		if !ok {
			return AgreementsView{}, agreementLookups{}, fmt.Errorf("%w: no version for proposal %s",
				ErrAgreementDocumentCorrupt, p.ID)
		}
		// Read with the comma-ok form, never map[key] straight into a field:
		// a miss would otherwise print another section's name under a change
		// nobody made there.
		sectionName, ok := lk.sectionNames[p.SectionID]
		if !ok {
			return AgreementsView{}, agreementLookups{}, fmt.Errorf("%w: accepted proposal %s names section %s",
				ErrAgreementDocumentCorrupt, p.ID, p.SectionID)
		}
		// A signer whose membership no longer resolves is OMITTED, never
		// joined as "", or history renders "Agreed by Andreas and ". Decision
		// 20 keeps the row forever, so this is the ordinary case for a
		// household a partner has left.
		signed := make([]string, 0, len(p.SignedByMembershipIDs))
		for _, id := range p.SignedByMembershipIDs {
			if name := lk.names[id]; name != "" {
				signed = append(signed, name)
			}
		}
		history = append(history, AgreementHistoryEntry{Version: version, ProposalID: p.ID,
			Kind: p.Kind, SectionID: p.SectionID, SectionName: sectionName,
			Body: p.Body, PreviousBody: p.PreviousBody, Note: p.Note,
			ProposedByName: lk.names[p.ProposedByMembershipID], SignedByNames: signed,
			AcceptedAt: *p.ResolvedAt})
	}

	// Locked gates writes and never hides the document (decision 3), and it
	// travels beside the owners list on every response -- a screen may only
	// say a household is locked once an answered query has said so.
	return AgreementsView{Locked: domain.AgreementsLocked(lk.all), Owners: owners,
		Version: len(doc.Accepted) + 1, UpdatedAt: updatedAt, Sections: sections,
		Proposals: proposals, History: history}, lk, nil
}

// proposalView composes one proposal. It is factored out of compose's walk
// over doc.Open because a write must return a row that has just become
// accepted or withdrawn -- rows that walk excludes in SQL -- and the row and
// the document in one response have to be composed by the same code.
func (s *AgreementService) proposalView(
	p AgreementProposalRecord,
	all []domain.Membership,
	names map[string]string,
	liveBody map[string]string,
	sectionNames map[string]string,
) (AgreementProposalView, error) {
	// Parsed here as well as in compose's open-slice walk: a write calls this
	// with a row that walk never saw, so each entry point fails closed on its
	// own rather than trusting the one before it.
	status, err := domain.ParseAgreementProposalStatus(p.Status)
	if err != nil {
		return AgreementProposalView{}, err
	}
	sectionName, ok := sectionNames[p.SectionID]
	if !ok {
		return AgreementProposalView{}, fmt.Errorf("%w: proposal %s names section %s",
			ErrAgreementDocumentCorrupt, p.ID, p.SectionID)
	}
	awaiting := domain.AwaitingSignature(all, p.SignedByMembershipIDs)
	awaitingNames := make([]string, 0, len(awaiting))
	for _, id := range awaiting {
		awaitingNames = append(awaitingNames, names[id])
	}
	// TargetChanged is the read-side echo of decision 13, whose authority
	// stays in Sign. It is asked ONLY of an open proposal: an accepted remove
	// has taken its own target out of the live set, so the literal rule would
	// answer true on the very change that just succeeded, and the write
	// response would read as "your agree was stale". A resolved proposal
	// cannot go stale -- the question it asked has been answered. An add has
	// no target, so an empty id also means "cannot go stale".
	targetChanged := false
	if status.IsOpen() && p.TargetAgreementID != "" {
		body, stillLive := liveBody[p.TargetAgreementID]
		targetChanged = !stillLive || body != p.PreviousBody
	}
	return AgreementProposalView{ID: p.ID, Kind: p.Kind, Status: p.Status,
		SectionID: p.SectionID, SectionName: sectionName, TargetAgreementID: p.TargetAgreementID,
		Body: p.Body, PreviousBody: p.PreviousBody, Note: p.Note, ParkNote: p.ParkNote,
		ProposedByMembershipID: p.ProposedByMembershipID,
		ProposedByName:         names[p.ProposedByMembershipID], ProposedAt: p.CreatedAt,
		AwaitingNames: awaitingNames, TargetChanged: targetChanged,
		SignedByMembershipIDs: p.SignedByMembershipIDs}, nil
}
```

- [ ] **Step 6: Run the tests and watch them pass** — `cd api && go test ./internal/usecase/ -run TestAgreement -count=1 -v`. Expected: PASS, three tests.

- [ ] **Step 7: Mutation-check the version derivation** — in `compose`'s return, change `Version: len(doc.Accepted) + 1` to `Version: len(doc.Accepted) + len(doc.Open) + 1`, the `count(*)` mistake written the way somebody actually makes it. Expected: `TestAgreementGetComposesTheWholeDocument` red on `Version = 6, want 4 -- count(accepted) + 1, not count(*)`, **and on nothing else** — the history versions come from `versionOf`, which the mutation does not touch. If another assertion fires too, the fixture is wired wrong. A test failure, not a build failure. Restore.

- [ ] **Step 8: Mutation-check the invisible section** — in the sections walk, change `Visible: len(lines) > 0` to `Visible: true`. Expected: the same test red on `Us = {Count 0, Visible true}, want {0, false} and still present`, and green on the Money assertion beside it, which asserts `Visible` true and would pass either way. This is the spec's own mutation and it only works because the fixture holds a section with no live agreements and the test asserts the flag rather than the array length — the section travels regardless. A test failure. Restore.

- [ ] **Step 9: Lint, then commit** — `make lint` first (arch lint, `go vet`); `go` is not on `PATH` in a bare shell here, so add `/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin` before running it.

```bash
git add api/internal/usecase/ports.go api/internal/usecase/agreement.go \
  api/internal/usecase/agreement_test.go api/internal/usecase/testdouble_test.go
git commit -F - <<'MSG'
feat(agreements): the port, and every figure the document displays

AgreementRepository and its record types, the seven view types, and
AgreementService.Get -- one walk deriving the version, the 01..N numbering,
the awaiting names, the history list and the two locked flags, and storing
none of them. proposalView is factored out of that walk so Task 4's writes can
compose a row the walk never sees: an accepted or withdrawn proposal is
excluded from doc.Open in SQL, and the row and the document in one response
must come from the same code.

Mutation checks, both restored afterwards:

- Version: len(doc.Accepted) + 1 -> len(doc.Accepted) + len(doc.Open) + 1.
  TestAgreementGetComposesTheWholeDocument went red on
  "Version = 6, want 4 -- count(accepted) + 1, not count(*)" and on nothing
  else. A TEST failure.
- Visible: len(lines) > 0 -> true.
  The same test went red on "Us = {Count 0, Visible true}, want {0, false}
  and still present". A TEST failure.

Step 3's run-and-watch-it-fail was a BUILD failure -- undefined:
newAgreementRepoDouble, undefined: usecase.NewAgreementService.
MSG
```

---

### Task 4: The six writes, and the two-owner gate that precedes them

**Files:**
- Modify: `api/internal/usecase/agreement.go` — append; add `"strings"` and `"unicode/utf8"` to its import block
- Modify: `api/internal/usecase/agreement_test.go` — append; add `"errors"`, `"strings"` and `"unicode/utf8"` to its import block

Nothing else. The double is complete as Task 3 left it, including `lastWithdrawBy`, which Step 7's mutation check reads.

**Interfaces:**

- **Consumes (Task 3), exactly:**

```go
func NewAgreementService(agreements AgreementRepository, members MembershipRepository) *AgreementService
func (s *AgreementService) Get(ctx context.Context, householdID string) (AgreementsView, error)
func (s *AgreementService) compose(ctx context.Context, householdID string) (AgreementsView, agreementLookups, error)
func (s *AgreementService) proposalView(p AgreementProposalRecord, all []domain.Membership,
	names, liveBody, sectionNames map[string]string) (AgreementProposalView, error)
type agreementLookups struct{ all []domain.Membership; names, liveBody, sectionNames map[string]string }
var ErrAgreementDocumentCorrupt error
// plus AgreementRepository and every record and view type Task 3 declared.
```

- **Consumes (Task 2):** `domain.ValidateAgreementSectionName(name string) error`, `domain.StarterSectionNames() []string`, `domain.AgreementProposal` and `(domain.AgreementProposal) Validate() error`, `domain.MaxAgreementParkNoteLen`, `domain.MaxAgreementNoteLen`, `domain.MinAgreementOwners`, `domain.AgreementsLocked`, `domain.ErrAgreementsNeedTwoOwners`, `domain.ErrAgreementParkNoteTooLong`, `domain.ErrAgreementNoteTooLong`, `domain.ErrAgreementNotOpen`, `domain.ErrForbidden`.
- **Produces, on `*AgreementService` — Tasks 7 and 8 call exactly these, and the plan header's interface map is where they come from:**

```go
func (s *AgreementService) CreateSection(ctx context.Context, householdID, name string, at time.Time) (AgreementSectionView, AgreementsView, error)
func (s *AgreementService) SeedStarterSections(ctx context.Context, householdID string, at time.Time) (AgreementsView, error)
func (s *AgreementService) Propose(ctx context.Context, householdID, proposedByMembershipID string, p domain.AgreementProposal, at time.Time) (AgreementProposalView, AgreementsView, error)
func (s *AgreementService) Sign(ctx context.Context, householdID, proposalID, membershipID string, at time.Time) (AgreementProposalView, AgreementsView, error)
func (s *AgreementService) Park(ctx context.Context, householdID, proposalID, note string, at time.Time) (AgreementProposalView, AgreementsView, error)
func (s *AgreementService) Withdraw(ctx context.Context, householdID, proposalID, byMembershipID string, at time.Time) (AgreementProposalView, AgreementsView, error)
func (s *AgreementService) Proposal(ctx context.Context, householdID, proposalID string) (AgreementProposalRecord, error)
```

**Every write returns the row it touched AND the whole recomposed document**, the shape `VisionService.Save` already uses (`api/internal/usecase/vision.go:117`) and the shape the spec requires: "Every write returns the whole document, because each moves the version, the `01..N` numbering, the history list and which proposals are open." `SeedStarterSections` returns the document alone — it creates labels, and there is no single row to name. `Proposal` is the one read here and is deliberately **not** gated, for the reason its doc comment gives.

One unexported helper carries the read-back for the four proposal writes; it is not in the header's map because nothing outside this file calls it:

```go
func (s *AgreementService) writtenProposal(ctx context.Context, householdID string,
	rec AgreementProposalRecord) (AgreementProposalView, AgreementsView, error)
```

- [ ] **Step 1: Write the failing tests** — append to `api/internal/usecase/agreement_test.go`.

```go
// Every write refuses a one-owner household, and refuses it BEFORE any
// repository call: the write counter is what separates "refused" from
// "refused eventually" (decision 1). Uniform on purpose -- a rule that let
// some writes through a locked household is one a reader gets wrong.
func TestAgreementWritesAreRefusedOnAOneOwnerHouseholdBeforeAnyRepositoryCall(t *testing.T) {
	members := seedMembers(t, 1)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	section := repo.seedSection("Money")
	open := repo.seedProposal("pending", "add", section.ID, "We split the holiday fund", nil, "m1")
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	for name, write := range map[string]func() error{
		"CreateSection": func() error { _, _, err := svc.CreateSection(ctx, "hh", "Faith", now); return err },
		"SeedStarterSections": func() error {
			_, err := svc.SeedStarterSections(ctx, "hh", now)
			return err
		},
		"Propose": func() error {
			_, _, err := svc.Propose(ctx, "hh", "m1", domain.AgreementProposal{
				Kind: "add", SectionID: section.ID, Body: "One night a week is ours"}, now)
			return err
		},
		"Sign":     func() error { _, _, err := svc.Sign(ctx, "hh", open.ID, "m1", now); return err },
		"Park":     func() error { _, _, err := svc.Park(ctx, "hh", open.ID, "next retro", now); return err },
		"Withdraw": func() error { _, _, err := svc.Withdraw(ctx, "hh", open.ID, "m1", now); return err },
	} {
		if err := write(); !errors.Is(err, domain.ErrAgreementsNeedTwoOwners) {
			t.Errorf("%s: err = %v, want ErrAgreementsNeedTwoOwners", name, err)
		}
	}
	if repo.writes != 0 {
		t.Fatalf("%d writes reached the repository, want 0 -- the gate ran after the call", repo.writes)
	}
}

// Proposing is agreeing (decision 5), and the signing set is every CURRENT
// owner (decision 4): an owner who joins mid-proposal must sign, one who
// leaves stops blocking it, and the proposal stays open through both. The
// write returns the row AND the recomposed document, and both are asserted
// here, because Task 7 maps the first and Task 8 answers with the second.
func TestAgreementProposeSignsTheProposerAndTheAwaitingListFollowsTheOwners(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	section := repo.seedSection("Us")
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	proposal, doc, err := svc.Propose(ctx, "hh", "m1", domain.AgreementProposal{
		Kind: "add", SectionID: section.ID, Body: "  One night a week is ours  "}, now)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if proposal.Body != "One night a week is ours" {
		t.Errorf("Body = %q -- the service trims before it validates, so what is stored is what was checked", proposal.Body)
	}
	if proposal.Status != "pending" || proposal.SectionName != "Us" || proposal.ProposedByName != "Andreas" {
		t.Errorf("the written row = {Status %q, SectionName %q, ProposedByName %q}, want {pending, Us, Andreas}",
			proposal.Status, proposal.SectionName, proposal.ProposedByName)
	}
	// The proposer's own signature landed with the proposal, so only the
	// other owner is awaited -- the design's "needs Christine". Both halves
	// of the response say so, from the one composition.
	if got := proposal.AwaitingNames; len(got) != 1 || got[0] != "Christine" {
		t.Fatalf("the written row awaits %v, want [Christine] -- the proposer signed implicitly", got)
	}
	if len(doc.Proposals) != 1 || doc.Proposals[0].ID != proposal.ID {
		t.Fatalf("the document returned with the write carries %d proposals, want the one just written", len(doc.Proposals))
	}
	if doc.Version != 1 {
		t.Errorf("Version = %d, want 1 -- a proposal nobody has agreed to has changed nothing", doc.Version)
	}

	awaiting := func() []string {
		t.Helper()
		view, err := svc.Get(ctx, "hh")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if len(view.Proposals) != 1 {
			t.Fatalf("len(Proposals) = %d, want 1 -- it waits, it is never filed away", len(view.Proposals))
		}
		return view.Proposals[0].AwaitingNames
	}
	members.users.put(usecase.StoredUser{User: domain.User{ID: "u-m3", DisplayName: "Priya"}})
	members.put(domain.Membership{ID: "m3", HouseholdID: "hh", UserID: "u-m3", Role: domain.RoleOwner})
	// Length only, never got[0]: membershipDouble.List ranges a Go map, so
	// the order domain.RequiredSigners sees here is not stable. The order
	// claim is the domain test's to pin, on a fixture it builds itself.
	if got := awaiting(); len(got) != 2 {
		t.Fatalf("awaiting = %v, want both non-proposers -- an owner who joins is bound by the promise", got)
	}
	if err := members.Delete(ctx, "hh", "m3"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got := awaiting(); len(got) != 1 || got[0] != "Christine" {
		t.Fatalf("awaiting = %v, want [Christine] -- an owner who leaves stops blocking it", got)
	}

	// The last awaiting signature applies the change: the proposal leaves the
	// open list, the version moves, and the agreement is numbered in place.
	signed, after, err := svc.Sign(ctx, "hh", proposal.ID, "m2", now)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if signed.Status != "accepted" || signed.TargetChanged {
		t.Errorf("the completing signature returned {Status %q, TargetChanged %v}, want {accepted, false}",
			signed.Status, signed.TargetChanged)
	}
	if len(after.Proposals) != 0 || after.Version != 2 || len(after.Sections[0].Agreements) != 1 {
		t.Fatalf("after the completing signature: %d open, v%d, %d agreements -- want 0, v2, 1",
			len(after.Proposals), after.Version, len(after.Sections[0].Agreements))
	}
	if got := after.Sections[0].Agreements[0].Number; got != 1 {
		t.Errorf("the new agreement is number %d, want 1", got)
	}
}

// The last awaiting signature applies an EDIT and a REMOVE, not only an add.
// An edit is a remove and an add together, so the agreement's id changes and
// the new wording sorts last in its section, exactly where created_at, id
// puts it -- which is why the assertions below find rows by body. The
// proposal keeps the OLD wording in PreviousBody, because that is what the
// history modal renders and what Restore pre-fills (decisions 13 and 18).
func TestAgreementSignAppliesAnEditAndARemoveAndRenumbersWhatIsLeft(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money, home := repo.seedSection("Money"), repo.seedSection("Home & kids")
	overTwoHundred := repo.seedAgreement(money.ID, "Anything over $200 gets a conversation")
	firstSunday := repo.seedAgreement(money.ID, "We review the budget on the first Sunday")
	repo.seedAgreement(home.ID, "Phone-free dinners")
	svc := usecase.NewAgreementService(repo, members)
	ctx := context.Background()
	proposedAt := time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)
	agreedAt := proposedAt.Add(time.Hour)

	edit, _, err := svc.Propose(ctx, "hh", "m1", domain.AgreementProposal{
		Kind:              "edit",
		TargetAgreementID: overTwoHundred.ID,
		PreviousBody:      overTwoHundred.Body,
		Body:              "Anything over $300 gets a conversation"}, proposedAt)
	if err != nil {
		t.Fatalf("Propose an edit: %v", err)
	}
	// The section came from the TARGET, not the caller: Validate refuses a
	// caller-supplied one on an edit, so this can only have been copied.
	if edit.SectionID != money.ID || edit.SectionName != "Money" {
		t.Fatalf("the edit landed in section %q (%q), want Money -- the target's section is copied onto the proposal",
			edit.SectionName, edit.SectionID)
	}
	if _, _, err := svc.Sign(ctx, "hh", edit.ID, "m2", agreedAt); err != nil {
		t.Fatalf("Sign the edit: %v", err)
	}

	remove, _, err := svc.Propose(ctx, "hh", "m1", domain.AgreementProposal{
		Kind:              "remove",
		TargetAgreementID: firstSunday.ID,
		PreviousBody:      firstSunday.Body}, proposedAt)
	if err != nil {
		t.Fatalf("Propose a remove: %v", err)
	}
	removed, doc, err := svc.Sign(ctx, "hh", remove.ID, "m2", agreedAt.Add(time.Hour))
	if err != nil {
		t.Fatalf("Sign the remove: %v", err)
	}
	if removed.Status != "accepted" || removed.TargetChanged {
		t.Errorf("the completing signature returned {Status %q, TargetChanged %v}, want {accepted, false} -- "+
			"a resolved proposal cannot be stale, and an accepted remove has taken its own target away",
			removed.Status, removed.TargetChanged)
	}

	bodies := func(sec usecase.AgreementSectionView) []string {
		out := make([]string, 0, len(sec.Agreements))
		for _, a := range sec.Agreements {
			out = append(out, a.Body)
		}
		return out
	}
	if got := bodies(doc.Sections[0]); len(got) != 1 || got[0] != "Anything over $300 gets a conversation" {
		t.Fatalf("Money holds %v, want only the edited wording -- the edit replaced a row, it did not add one", got)
	}
	// Numbering runs continuously across sections and counts only live rows:
	// Money 01, Home 02, with two removed rows numbered nowhere.
	if doc.Sections[0].Agreements[0].Number != 1 || doc.Sections[1].Agreements[0].Number != 2 {
		t.Errorf("numbers are %d then %d, want 1 then 2 -- a removed row is not numbered and not skipped over",
			doc.Sections[0].Agreements[0].Number, doc.Sections[1].Agreements[0].Number)
	}
	if doc.Version != 3 || len(doc.History) != 2 {
		t.Fatalf("v%d with %d history entries, want v3 and 2", doc.Version, len(doc.History))
	}
	// History is newest first, and a removal renders wording that is nowhere
	// else in the document any more -- "you can always see it was there and
	// restore it later".
	if doc.History[0].Kind != "remove" || doc.History[0].Body != "" ||
		doc.History[0].PreviousBody != "We review the budget on the first Sunday" {
		t.Errorf("history[0] = {Kind %q, Body %q, PreviousBody %q}, want the removal carrying the old wording",
			doc.History[0].Kind, doc.History[0].Body, doc.History[0].PreviousBody)
	}
	if doc.History[1].Kind != "edit" || doc.History[1].PreviousBody != "Anything over $200 gets a conversation" {
		t.Errorf("history[1] = {Kind %q, PreviousBody %q}, want the edit carrying the wording it replaced",
			doc.History[1].Kind, doc.History[1].PreviousBody)
	}
}

// Withdrawing something already accepted is the last signer double-clicking
// through a stale page. It means "reload, this was settled", and it must
// write nothing: an accepted proposal that flipped to withdrawn would leave
// its agreement live with no record of how it got there.
func TestAgreementWithdrawOnAnAcceptedProposalWritesNothing(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money := repo.seedSection("Money")
	repo.seedAgreement(money.ID, "Anything over $200 gets a conversation")
	resolved := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	accepted := repo.seedProposal("accepted", "add", money.ID,
		"Anything over $200 gets a conversation", &resolved, "m1", "m2")
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	before := repo.writes
	_, _, err := svc.Withdraw(ctx, "hh", accepted.ID, "m1", now)
	if !errors.Is(err, domain.ErrAgreementNotOpen) {
		t.Fatalf("err = %v, want ErrAgreementNotOpen -- reload, this was settled", err)
	}
	if repo.writes != before {
		t.Errorf("writes moved from %d to %d -- a refusal changed a row", before, repo.writes)
	}
	view, err := svc.Get(ctx, "hh")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if view.Version != 2 || len(view.History) != 1 {
		t.Errorf("v%d with %d history entries, want v2 and 1 -- the refused withdraw rewrote history", view.Version, len(view.History))
	}
}

// Withdraw hands byMembershipID to the store and never branches on it: the
// proposer check is the HANDLER's, because only the HTTP layer knows who is
// asking (decision 15, inside decision 22's 404 -> 403 -> 409 order). The
// proof is not that a stranger is refused -- it is that the refusal came back
// FROM the repository, which means the service reached it.
func TestAgreementWithdrawHandsTheProposerCheckToTheStore(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money := repo.seedSection("Money")
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	proposed, _, err := svc.Propose(ctx, "hh", "m1", domain.AgreementProposal{
		Kind: "add", SectionID: money.ID, Body: "We split the holiday fund"}, now)
	if err != nil {
		t.Fatalf("Propose: %v", err)
	}
	if _, _, err := svc.Withdraw(ctx, "hh", proposed.ID, "m2", now); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden -- the store's own backstop clause", err)
	}
	if repo.lastWithdrawBy != "m2" {
		t.Fatalf("the repository saw byMembershipID %q, want %q -- the service decided instead of asking",
			repo.lastWithdrawBy, "m2")
	}

	withdrawn, doc, err := svc.Withdraw(ctx, "hh", proposed.ID, "m1", now)
	if err != nil {
		t.Fatalf("Withdraw by the proposer: %v", err)
	}
	if withdrawn.Status != "withdrawn" || len(doc.Proposals) != 0 {
		t.Errorf("the written row is %q with %d open proposals, want withdrawn and 0",
			withdrawn.Status, len(doc.Proposals))
	}
	if doc.Version != 1 || len(doc.History) != 0 {
		t.Errorf("v%d with %d history entries, want v1 and none -- a withdrawal is not a change to the document",
			doc.Version, len(doc.History))
	}
}

// "Use starter set" seeds four labels and nothing else (decision 17), so
// "everything on this page is here because you both agreed" stays literally
// true. A second click is a no-op, not a 409: ON CONFLICT DO NOTHING, and the
// write counter is what proves it.
func TestAgreementStarterSetIsIdempotentAndCreatesNoAgreements(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	first, err := svc.SeedStarterSections(ctx, "hh", now)
	if err != nil {
		t.Fatalf("SeedStarterSections: %v", err)
	}
	want := domain.StarterSectionNames()
	if len(first.Sections) != len(want) {
		t.Fatalf("%d sections, want %d", len(first.Sections), len(want))
	}
	for i, sec := range first.Sections {
		if sec.Name != want[i] {
			t.Errorf("section %d is %q, want %q", i, sec.Name, want[i])
		}
		if sec.Count != 0 || sec.Visible || len(sec.Agreements) != 0 {
			t.Errorf("%q arrived with %d agreements -- the starter set seeds labels, never promises",
				sec.Name, sec.Count)
		}
	}
	if first.Version != 1 {
		t.Errorf("Version = %d, want 1 -- seeding sections agrees to nothing", first.Version)
	}

	afterFirst := repo.writes
	second, err := svc.SeedStarterSections(ctx, "hh", now)
	if err != nil {
		t.Fatalf("SeedStarterSections a second time: %v -- a second click is a no-op, not a 409", err)
	}
	if len(second.Sections) != len(want) {
		t.Errorf("%d sections after the second click, want %d -- it duplicated or dropped labels", len(second.Sections), len(want))
	}
	if repo.writes != afterFirst {
		t.Errorf("%d writes on the second click, want 0", repo.writes-afterFirst)
	}
}

// The park note is capped separately from the proposal note: two fields on
// two screens, and one constant serving both would have to move for both.
// Both constants are 500, so no length assertion can tell them apart -- only
// the SENTINEL can, which is why this test asserts one and refuses the other.
func TestAgreementParkCapsItsOwnNoteSeparatelyFromTheProposalNote(t *testing.T) {
	members := seedMembers(t, 2)
	repo := newAgreementRepoDouble()
	repo.setMembers(members)
	money := repo.seedSection("Money")
	open := repo.seedProposal("pending", "add", money.ID, "We split the holiday fund", nil, "m1")
	svc := usecase.NewAgreementService(repo, members)
	ctx, now := context.Background(), time.Date(2026, 9, 5, 20, 0, 0, 0, time.UTC)

	// Three bytes to the rune, so a byte cap and a rune cap disagree about
	// this fixture: len() refuses the note that must be accepted.
	atCap := strings.Repeat("我", domain.MaxAgreementParkNoteLen)

	parked, _, err := svc.Park(ctx, "hh", open.ID, "  "+atCap+"  ", now)
	if err != nil {
		t.Fatalf("Park at the cap: %v -- %d runes is inside MaxAgreementParkNoteLen whatever it is in bytes",
			err, domain.MaxAgreementParkNoteLen)
	}
	if utf8.RuneCountInString(parked.ParkNote) != domain.MaxAgreementParkNoteLen {
		t.Errorf("stored park note is %d runes, want %d -- trimmed before it was measured",
			utf8.RuneCountInString(parked.ParkNote), domain.MaxAgreementParkNoteLen)
	}
	if parked.Status != "parked" {
		t.Errorf("Status = %q, want parked -- Discuss leaves the proposal open (decision 7)", parked.Status)
	}

	writes := repo.writes
	_, _, err = svc.Park(ctx, "hh", open.ID, atCap+"我", now)
	if !errors.Is(err, domain.ErrAgreementParkNoteTooLong) {
		t.Errorf("err = %v, want ErrAgreementParkNoteTooLong", err)
	}
	if errors.Is(err, domain.ErrAgreementNoteTooLong) {
		t.Error("Park answered ErrAgreementNoteTooLong -- a tidy-up aliased the park note's cap to the proposal note's, " +
			"which no length assertion can see because both constants are 500")
	}
	if repo.writes != writes {
		t.Errorf("writes moved from %d to %d -- an over-long note reached the repository", writes, repo.writes)
	}
}
```

- [ ] **Step 2: Run them and watch them fail** — `cd api && go test ./internal/usecase/ -run TestAgreement -count=1 -v`. Expected, roughly:

```
# github.com/andreasoentoro/hearth/api/internal/usecase_test [build failed]
internal/usecase/agreement_test.go:NNN:38: svc.CreateSection undefined (type *usecase.AgreementService has no field or method CreateSection)
internal/usecase/agreement_test.go:NNN:29: svc.SeedStarterSections undefined (type *usecase.AgreementService has no field or method SeedStarterSections)
internal/usecase/agreement_test.go:NNN:32: svc.Propose undefined (type *usecase.AgreementService has no field or method Propose)
...
FAIL	github.com/andreasoentoro/hearth/api/internal/usecase [build failed]
```

A **build** failure, not a test failure. Note which you saw; Step 8's commit body asks for it. Task 3's three tests do not run at all while the package will not build — that is expected, and it is why Step 4 counts all ten.

- [ ] **Step 3: Write the gate, the read-back and the six writes** — append to `api/internal/usecase/agreement.go`, adding `"strings"` and `"unicode/utf8"` to its import block.

```go
// requireTwoOwners is decision 1's gate, and it runs FIRST in every write --
// uniform on purpose, because a rule that let some writes through a locked
// household is one a reader gets wrong. An open proposal simply waits for a
// second owner; there is no expiry and no decline (decision 6).
func (s *AgreementService) requireTwoOwners(ctx context.Context, householdID string) error {
	views, err := s.members.List(ctx, householdID)
	if err != nil {
		return err
	}
	if domain.AgreementsLocked(membershipsFrom(views)) {
		return domain.ErrAgreementsNeedTwoOwners
	}
	return nil
}

// writtenProposal is the read-back every proposal write ends with: compose
// the document again and run the row that was just written through the same
// proposalView the document's own open proposals went through. One
// composition, so the row and the document in a single response cannot
// describe the household differently -- and so an accepted or withdrawn row,
// which the document's own walk excludes in SQL, still gets composed by the
// code that knows how.
//
// The document is a snapshot taken AFTER the write and outside its
// transaction, so a concurrent agree may already have overtaken it; the
// frontend's refetch stays the authority. A write that lands and then cannot
// be read back is an error, not a silent success: the row is committed, and
// the caller is owed a 500 rather than a half-answer.
func (s *AgreementService) writtenProposal(ctx context.Context, householdID string,
	rec AgreementProposalRecord) (AgreementProposalView, AgreementsView, error) {
	doc, lk, err := s.compose(ctx, householdID)
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	view, err := s.proposalView(rec, lk.all, lk.names, lk.liveBody, lk.sectionNames)
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	return view, doc, nil
}

// CreateSection adds one label. It never pre-checks the name: the unique index
// decides the collision, and a "does this name already exist" read is a
// check-then-write two owners can both pass (decision 19). The section comes
// back as the recomposed document numbers and flags it, so the row in the
// response and the same row inside `agreements` can never disagree.
func (s *AgreementService) CreateSection(ctx context.Context, householdID, name string,
	at time.Time) (AgreementSectionView, AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementSectionView{}, AgreementsView{}, err
	}
	name = strings.TrimSpace(name) // trim first, so what is stored is what was validated
	if err := domain.ValidateAgreementSectionName(name); err != nil {
		return AgreementSectionView{}, AgreementsView{}, err
	}
	rec, err := s.agreements.CreateSection(ctx, householdID, name, at)
	if err != nil {
		return AgreementSectionView{}, AgreementsView{}, err
	}
	doc, _, err := s.compose(ctx, householdID)
	if err != nil {
		return AgreementSectionView{}, AgreementsView{}, err
	}
	for _, sec := range doc.Sections {
		if sec.ID == rec.ID {
			return sec, doc, nil
		}
	}
	return AgreementSectionView{}, AgreementsView{}, fmt.Errorf(
		"%w: section %s is missing from the document that just created it",
		ErrAgreementDocumentCorrupt, rec.ID)
}

// SeedStarterSections is "Use starter set": four labels and no agreements
// (decision 17), so "everything here is here because you both agreed" stays
// literally true. Idempotent -- a second click is a no-op, not a 409. It
// answers with the document alone: it creates labels, and there is no single
// row to name. The repository's read-back proves all four landed; nothing
// renders from that slice, because render order is always the document's.
func (s *AgreementService) SeedStarterSections(ctx context.Context, householdID string,
	at time.Time) (AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementsView{}, err
	}
	if _, err := s.agreements.CreateSections(ctx, householdID, domain.StarterSectionNames(), at); err != nil {
		return AgreementsView{}, err
	}
	return s.Get(ctx, householdID)
}

// Propose stamps the household and the proposer from the route and the session
// BEFORE validating, so a body naming another household is judged against its
// own constraints rather than smuggled past them -- VisionService.Save's own
// reasoning. The target check is deliberately not repeated here: it can only
// be made atomically inside CreateProposal's transaction.
func (s *AgreementService) Propose(ctx context.Context, householdID, proposedByMembershipID string,
	p domain.AgreementProposal, at time.Time) (AgreementProposalView, AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	p.HouseholdID, p.ProposedByMembershipID = householdID, proposedByMembershipID
	p.Body, p.PreviousBody = strings.TrimSpace(p.Body), strings.TrimSpace(p.PreviousBody)
	p.Note = strings.TrimSpace(p.Note)
	if err := p.Validate(); err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	rec, err := s.agreements.CreateProposal(ctx, AgreementProposalWrite{HouseholdID: p.HouseholdID,
		Kind: p.Kind, SectionID: p.SectionID, TargetAgreementID: p.TargetAgreementID, Body: p.Body,
		PreviousBody: p.PreviousBody, Note: p.Note,
		ProposedByMembershipID: p.ProposedByMembershipID, CreatedAt: at})
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	return s.writtenProposal(ctx, householdID, rec)
}

// Sign validates nothing beyond the gate: every count and comparison that
// decides the outcome -- the owner count, the signatures held by current
// owners, the target's wording -- is inside the repository's transaction,
// which is the only place any of them is atomic (decisions 4 and 13).
func (s *AgreementService) Sign(ctx context.Context, householdID, proposalID, membershipID string,
	at time.Time) (AgreementProposalView, AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	rec, err := s.agreements.Sign(ctx, AgreementSignatureWrite{HouseholdID: householdID,
		ProposalID: proposalID, MembershipID: membershipID, At: at})
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	return s.writtenProposal(ctx, householdID, rec)
}

// Park is Discuss: the proposal stays open and shows on the Retros page's
// To-discuss block (decision 7). The note is capped in RUNES, never bytes, or
// a household writing Chinese gets a third of what the modal promised -- and
// in MaxAgreementParkNoteLen, not the proposal note's cap: two fields on two
// screens, and one constant serving both would have to move for both.
func (s *AgreementService) Park(ctx context.Context, householdID, proposalID, note string,
	at time.Time) (AgreementProposalView, AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	note = strings.TrimSpace(note) // trimmed before it is measured, as Validate's own contract says
	if utf8.RuneCountInString(note) > domain.MaxAgreementParkNoteLen {
		return AgreementProposalView{}, AgreementsView{}, domain.ErrAgreementParkNoteTooLong
	}
	rec, err := s.agreements.Park(ctx, householdID, proposalID, note, at)
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	return s.writtenProposal(ctx, householdID, rec)
}

// Withdraw does not branch on byMembershipID, and must not: the proposer check
// is the handler's, because only the HTTP layer knows who is asking (decision
// 15, in decision 22's 404 -> 403 -> 409 order). The id travels so the
// repository's own WHERE-clause backstop can apply it, and a refusal that
// comes back from there is the store's answer, not this method's.
func (s *AgreementService) Withdraw(ctx context.Context, householdID, proposalID,
	byMembershipID string, at time.Time) (AgreementProposalView, AgreementsView, error) {
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	rec, err := s.agreements.Withdraw(ctx, householdID, proposalID, byMembershipID, at)
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	return s.writtenProposal(ctx, householdID, rec)
}

// Proposal is a read, and is deliberately NOT gated by requireTwoOwners: the
// withdraw handler needs 404 before 403 before 409 (decision 22), and a gate
// here would answer 409 for a proposal that does not exist. Pair it with Get's
// Owners list, which is what decides whether the proposer is still one.
func (s *AgreementService) Proposal(ctx context.Context, householdID,
	proposalID string) (AgreementProposalRecord, error) {
	return s.agreements.Proposal(ctx, householdID, proposalID)
}
```

- [ ] **Step 4: Run the tests and watch them pass** — `cd api && go test ./internal/usecase/ -run TestAgreement -count=1 -v`. Expected: PASS, **ten** tests — Task 3's three and the seven above. Then `make lint` (arch lint, `go vet`); `go` is not on `PATH` in a bare shell here, so add `/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin` first.

- [ ] **Step 5: Mutation-check the ordering of the gate** — in `Propose`, move the write above the gate. Delete the `requireTwoOwners` block from the top of the method and put it back **after** the repository call, so the gate still runs and still returns the same error:

```go
	rec, err := s.agreements.CreateProposal(ctx, AgreementProposalWrite{ /* unchanged */ })
	if err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	if err := s.requireTwoOwners(ctx, householdID); err != nil {
		return AgreementProposalView{}, AgreementsView{}, err
	}
	return s.writtenProposal(ctx, householdID, rec)
```

Expected: `TestAgreementWritesAreRefusedOnAOneOwnerHouseholdBeforeAnyRepositoryCall` stays green on all six `errors.Is` lines — `Propose` still answers `ErrAgreementsNeedTwoOwners` — and goes red on the last assertion, `1 writes reached the repository, want 0 -- the gate ran after the call`. **Moving the block without moving the write is not this mutation:** the gate returns before `CreateProposal` runs either way, `repo.writes` stays 0, and the test stays green. Deleting the gate instead makes six assertions fire and proves less. A test failure. Restore.

- [ ] **Step 6: Mutation-check the park note's rune cap** — in `Park`, change `utf8.RuneCountInString(note)` to `len(note)`. Expected: `TestAgreementParkCapsItsOwnNoteSeparatelyFromTheProposalNote` red on the note that must be **accepted**, not on the over-long one — the first `t.Fatalf`, `Park at the cap: <ErrAgreementParkNoteTooLong's text> -- 500 runes is inside MaxAgreementParkNoteLen whatever it is in bytes` — because 500 Chinese runes are 1500 bytes. Being a `Fatalf`, it stops the test there and the over-long leg never runs; that leg would refuse under a byte cap too, which is exactly why the boundary fixture is multi-byte. An ASCII fixture would pass a byte cap and see nothing at all. A test failure. Restore.

- [ ] **Step 7: Mutation-check the proposer's implicit signature** (the spec's own
mutation 2) — in `CreateProposal`'s double, delete the line that records the
proposer's own signature, leaving the proposal row written.

Expected: a **test** failure on the awaiting list, not on the proposal:

```
--- FAIL: TestAgreementProposeSignsTheProposerAndTheAwaitingListFollowsTheOwners
    agreement_test.go:2384: awaitingNames = [Andreas Christine], want [Christine]
    -- the proposer is waiting for their own proposal
```

If the atomicity test in Task 6 reddens instead, the two rows are being written
but not together, which is a different defect with a different fix. Restore the
line.

- [ ] **Step 8: Mutation-check that `Withdraw` does not decide** — in `Withdraw`, insert a proposer check above the repository call:

```go
	if rec, err := s.agreements.Proposal(ctx, householdID, proposalID); err == nil &&
		rec.ProposedByMembershipID != byMembershipID {
		return AgreementProposalView{}, AgreementsView{}, domain.ErrForbidden
	}
```

Expected: `TestAgreementWithdrawHandsTheProposerCheckToTheStore` stays green on the `errors.Is(err, domain.ErrForbidden)` line — the answer is identical — and goes red on `the repository saw byMembershipID "", want "m2" -- the service decided instead of asking`. That is the whole point of the test: it pins **where** the refusal was made, not what it said, and a check moved up here would silently break decision 22's `404` → `403` → `409` order the moment a proposal did not exist. A test failure. Restore.

- [ ] **Step 9: Commit**

```bash
git add api/internal/usecase/agreement.go api/internal/usecase/agreement_test.go
git commit -F - <<'MSG'
feat(agreements): propose, sign, park, withdraw, and the two-owner gate

Six writes, each returning the row it touched and the whole recomposed
document (VisionService.Save's shape), composed through the same walk Get
uses, so the row and the document in one response cannot disagree.
requireTwoOwners runs first in every one of them; Proposal is deliberately
ungated so the withdraw handler can answer 404 before 403 before 409.

Mutation checks, all three restored afterwards:

- Propose: the CreateProposal call moved ABOVE requireTwoOwners, the gate kept.
  TestAgreementWritesAreRefusedOnAOneOwnerHouseholdBeforeAnyRepositoryCall
  stayed green on all six errors.Is legs and went red on
  "1 writes reached the repository, want 0 -- the gate ran after the call".
  A TEST failure. Moving only the gate block is inert: it returns before the
  write either way, and the counter never moves.
- Park: utf8.RuneCountInString(note) -> len(note).
  TestAgreementParkCapsItsOwnNoteSeparatelyFromTheProposalNote went red on the
  note that must be ACCEPTED -- 500 Chinese runes are 1500 bytes -- and not on
  the over-long one, which a byte cap refuses too. A TEST failure.
- Withdraw: a proposer check added above the repository call.
  TestAgreementWithdrawHandsTheProposerCheckToTheStore stayed green on the
  ErrForbidden assertion and went red on "the repository saw byMembershipID
  "", want "m2"". A TEST failure.

Step 2's run-and-watch-it-fail was a BUILD failure -- svc.CreateSection
undefined (type *usecase.AgreementService has no field or method
CreateSection), and the same for the other five.
MSG
```

---

### Task 5: The document repository

**Files:**
- Create: `api/internal/adapter/postgres/queries/agreements.sql`, `api/internal/adapter/postgres/agreement_repo.go`, `api/internal/adapter/postgres/agreement_repo_test.go`
- Modify: `api/internal/adapter/postgres/user_repo.go` (one more constraint const after `billNameUniqueConstraint` on line 198, and one more `translate` case immediately **above** the bare-23505 arm on line 235 — `errors.Is`/`errors.As` match in source order), `api/internal/adapter/postgres/sqlcgen/**` (regenerated by `make sqlc`, never hand-edited)

**Interfaces:**

- Consumes, from Task 1: the tables `agreement_sections`, `agreement_proposals`, `agreements`, `agreement_signatures`, the constraint name `agreement_sections_household_id_name_key`, and the CHECKs `agreement_proposals_shape`, `agreement_proposals_resolution_matches_status`, `agreements_removal_is_whole`.
- Consumes, from Task 2:

```go
func ParseAgreementProposalKind(s string) (AgreementProposalKind, error)
func ParseAgreementProposalStatus(s string) (AgreementProposalStatus, error)
var ErrAgreementSectionNameTaken error
```

- Consumes, from Task 3 (copy verbatim; invent nothing):

```go
type AgreementSectionRecord struct{ ID, Name string; CreatedAt time.Time }
type AgreementRecord struct{ ID, SectionID, Body, AddedByProposalID string; CreatedAt time.Time }
type AgreementProposalRecord struct {
    ID, Kind, Status, SectionID, TargetAgreementID, Body, PreviousBody, Note, ParkNote,
    ProposedByMembershipID string
    CreatedAt              time.Time
    ResolvedAt             *time.Time
    SignedByMembershipIDs  []string
}
type AgreementDocument struct {
    Sections   []AgreementSectionRecord
    Agreements []AgreementRecord          // live only, removed_at IS NULL
    Open       []AgreementProposalRecord  // pending and parked, created_at asc
    Accepted   []AgreementProposalRecord  // accepted only, resolved_at asc
}
```

- Produces, in `package postgres`:

```go
func NewAgreementRepo(db *DB) *AgreementRepo
func (r *AgreementRepo) Document(ctx context.Context, householdID string) (usecase.AgreementDocument, error)
func (r *AgreementRepo) Proposal(ctx context.Context, householdID, proposalID string) (usecase.AgreementProposalRecord, error)
func (r *AgreementRepo) CreateSection(ctx context.Context, householdID, name string, createdAt time.Time) (usecase.AgreementSectionRecord, error)
func (r *AgreementRepo) CreateSections(ctx context.Context, householdID string, names []string, createdAt time.Time) ([]usecase.AgreementSectionRecord, error)

// unexported, and Task 6 calls both:
func toAgreementProposal(row sqlcgen.GetAgreementProposalRow) (usecase.AgreementProposalRecord, error)
func membershipIDs(ids []pgtype.UUID) []string
```

  Task 6 hangs its four transactional methods off the **same** `*AgreementRepo`, which is why this one already holds the pool. `*AgreementRepo` does **not** satisfy `usecase.AgreementRepository` until Task 6 lands, so the compile-time assertion in `convert.go` is Task 6's step, not this one's.

- Produces, in `package postgres_test`, for Task 6's file to reuse: `at(sec int) time.Time`, `newAgreementRepo(t)`, `insertTestProposal`, `insertTestAgreement`, `insertTestSignature`.

- [ ] **Step 1: Write the failing tests**

Create `api/internal/adapter/postgres/agreement_repo_test.go`. `CreateProposal` is Task 6's, so proposals, agreements and signatures are seeded with raw SQL here — a repository test that builds its fixtures through the code under test fails for two reasons at once. Every migration CHECK has to be satisfied by hand or the first run fails on a constraint rather than on the missing repository.

```go
package postgres_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// at gives every agreement fixture a deterministic instant, with only the
// second varying, so an ordering assertion reads as a sequence.
//
// `grep -n 'func at(' internal/adapter/postgres/*_test.go` finds nothing else
// today; package postgres_test is one namespace across thirty files, so check
// before adding any two-letter helper to it.
func at(sec int) time.Time { return time.Date(2026, 9, 5, 9, 0, sec, 0, time.UTC) }

// newAgreementRepo opens a fresh database (one container per test, this
// package's convention), one household, and one "Money" section created
// THROUGH the repository at at(0). It returns the *postgres.DB as well as the
// repository because these tests seed proposals, agreements and signatures
// with raw SQL: the methods that write them are Task 6's.
//
// openTestDB is schema_test.go:1029 and insertTestHousehold is
// schema_test.go:1018; insertTestMembership (transaction_repo_test.go:38)
// returns an OWNER's membership id, which is what every signing assertion
// below depends on.
func newAgreementRepo(t *testing.T) (*postgres.AgreementRepo, *postgres.DB, string, string) {
	t.Helper()
	db := openTestDB(t)
	repo := postgres.NewAgreementRepo(db)
	householdID := insertTestHousehold(t, db)
	section, err := repo.CreateSection(context.Background(), householdID, "Money", at(0))
	if err != nil {
		t.Fatalf("CreateSection: %v", err)
	}
	return repo, db, householdID, section.ID
}

// insertTestProposal seeds one 'add' proposal. agreement_proposals_shape
// requires an add to carry target_agreement_id NULL, body <> '' and
// previous_body = '', and agreement_proposals_resolution_matches_status
// requires resolved_at exactly when the status is accepted or withdrawn --
// hence the *time.Time, which pgx encodes as SQL NULL when it is nil.
func insertTestProposal(t *testing.T, db *postgres.DB, householdID, sectionID, byMembershipID,
	status, body string, createdAt time.Time, resolvedAt *time.Time) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO agreement_proposals (household_id, kind, status, section_id, body,
		     previous_body, note, proposed_by_membership_id, created_at, resolved_at)
		 VALUES ($1, 'add', $2, $3, $4, '', '', $5, $6, $7) RETURNING id`,
		householdID, status, sectionID, body, byMembershipID, createdAt, resolvedAt).Scan(&id)
	if err != nil {
		t.Fatalf("insert %s proposal %q: %v", status, body, err)
	}
	return id
}

// insertTestAgreement seeds one row of the living document. removed_at and
// removed_by_proposal_id move together or agreements_removal_is_whole refuses
// the row -- "a removal is one event, not half of one" -- so the second stamp
// is written as a CASE over the first rather than as a parameter a caller
// could set independently. The proposal that added the row stands in as the
// one that removed it, because nothing here reads which proposal did the
// removing; what is under test is that both columns move together. The
// ::timestamptz cast is what lets Postgres type a NULL parameter inside CASE.
func insertTestAgreement(t *testing.T, db *postgres.DB, householdID, sectionID, proposalID,
	body string, createdAt time.Time, removedAt *time.Time) string {
	t.Helper()
	var id string
	err := db.Pool().QueryRow(context.Background(),
		`INSERT INTO agreements (household_id, section_id, body, added_by_proposal_id,
		     created_at, removed_at, removed_by_proposal_id)
		 VALUES ($1, $2, $3, $4, $5, $6,
		         CASE WHEN $6::timestamptz IS NULL THEN NULL ELSE $4::uuid END)
		 RETURNING id`,
		householdID, sectionID, body, proposalID, createdAt, removedAt).Scan(&id)
	if err != nil {
		t.Fatalf("insert agreement %q: %v", body, err)
	}
	return id
}

func insertTestSignature(t *testing.T, db *postgres.DB, proposalID, membershipID string, signedAt time.Time) {
	t.Helper()
	_, err := db.Pool().Exec(context.Background(),
		`INSERT INTO agreement_signatures (proposal_id, membership_id, signed_at) VALUES ($1, $2, $3)`,
		proposalID, membershipID, signedAt)
	if err != nil {
		t.Fatalf("insert signature: %v", err)
	}
}

// Two orders in one document: Open by created_at then id -- the tie is what
// proves the second key is there at all -- and Accepted by resolved_at,
// because a version is the k-th ACCEPTANCE and B can be accepted before A.
func TestAgreementDocumentOrdersOpenByCreatedAtAndAcceptedByResolvedAt(t *testing.T) {
	repo, db, h, sec := newAgreementRepo(t)
	alex := insertTestMembership(t, db, h, "Alex")
	first := insertTestProposal(t, db, h, sec, alex, "pending", "A", at(1), nil)
	second := insertTestProposal(t, db, h, sec, alex, "pending", "B", at(1), nil)
	third := insertTestProposal(t, db, h, sec, alex, "parked", "C", at(2), nil)
	late, early := at(6), at(5)
	createdFirst := insertTestProposal(t, db, h, sec, alex, "accepted", "D", at(3), &late)
	createdSecond := insertTestProposal(t, db, h, sec, alex, "accepted", "E", at(4), &early)

	doc, err := repo.Document(context.Background(), h)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if len(doc.Open) != 3 || len(doc.Accepted) != 2 {
		t.Fatalf("open = %d accepted = %d, want 3 and 2", len(doc.Open), len(doc.Accepted))
	}

	tied := []string{first, second}
	slices.Sort(tied) // uuid byte order is this string's order: fixed-width lowercase hex
	want := append(tied, third)
	got := []string{doc.Open[0].ID, doc.Open[1].ID, doc.Open[2].ID}
	if !slices.Equal(got, want) {
		t.Fatalf("open = %v, want %v (created_at then id)", got, want)
	}
	if doc.Accepted[0].ID != createdSecond || doc.Accepted[1].ID != createdFirst {
		t.Fatalf("accepted = %v, want %s then %s -- ordered by resolved_at, not created_at",
			[]string{doc.Accepted[0].ID, doc.Accepted[1].ID}, createdSecond, createdFirst)
	}
}

// Withdrawn proposals are excluded IN SQL and a removed agreement leaves the
// live document while keeping its row (decision 9) -- but Proposal still
// answers for the withdrawn one, which is the whole reason that method
// exists. The signature assertions are here because this is the only place
// the LEFT JOIN + array_agg is read: without the FILTER clause an unsigned
// proposal comes back holding one NULL rather than nothing.
func TestAgreementDocumentExcludesWithdrawnProposalsAndRemovedAgreements(t *testing.T) {
	ctx := context.Background()
	repo, db, h, sec := newAgreementRepo(t)
	alex := insertTestMembership(t, db, h, "Alex")
	resolved := at(4)
	withdrawn := insertTestProposal(t, db, h, sec, alex, "withdrawn", "we skip date night", at(1), &resolved)
	accepted := insertTestProposal(t, db, h, sec, alex, "accepted", "we save 20%", at(2), &resolved)
	insertTestSignature(t, db, accepted, alex, at(3))
	removedAt := at(5)
	insertTestAgreement(t, db, h, sec, accepted, "we save 20%", at(4), nil)
	insertTestAgreement(t, db, h, sec, accepted, "we skip date night", at(4), &removedAt)

	doc, err := repo.Document(ctx, h)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	for _, p := range append(append([]usecase.AgreementProposalRecord{}, doc.Open...), doc.Accepted...) {
		if p.ID == withdrawn {
			t.Fatalf("the withdrawn proposal reached the document as %q -- it must be excluded in SQL", p.Status)
		}
	}
	if len(doc.Agreements) != 1 || doc.Agreements[0].Body != "we save 20%" {
		t.Fatalf("live agreements = %v, want only the one that was never removed", doc.Agreements)
	}
	if len(doc.Accepted) != 1 || !slices.Equal(doc.Accepted[0].SignedByMembershipIDs, []string{alex}) {
		t.Fatalf("accepted signatures = %v, want exactly [%s]", doc.Accepted, alex)
	}

	// The row is still there, and Proposal is what reads it: the withdraw
	// handler's proposer check costs one query rather than a composed
	// document.
	rec, err := repo.Proposal(ctx, h, withdrawn)
	if err != nil {
		t.Fatalf("Proposal(withdrawn): %v", err)
	}
	if rec.Status != "withdrawn" || rec.ResolvedAt == nil || !rec.ResolvedAt.Equal(resolved) {
		t.Fatalf("status = %q resolvedAt = %v, want withdrawn at %s", rec.Status, rec.ResolvedAt, resolved)
	}
	if rec.SignedByMembershipIDs == nil {
		t.Fatalf("SignedByMembershipIDs = nil on an unsigned proposal, want an empty non-nil slice")
	}
}

// Decision 19: the collision is named, not the generic ErrAlreadyExists. The
// second leg goes round Go entirely, pinning the const to the migration's
// real constraint name rather than to what this package believes it is.
func TestCreateSectionTwiceIsAgreementSectionNameTaken(t *testing.T) {
	ctx := context.Background()
	repo, db, h, _ := newAgreementRepo(t) // already created "Money" at at(0)

	if _, err := repo.CreateSection(ctx, h, "Money", at(1)); !errors.Is(err, domain.ErrAgreementSectionNameTaken) {
		t.Fatalf("err = %v, want ErrAgreementSectionNameTaken", err)
	}

	_, err := db.Pool().Exec(ctx,
		`INSERT INTO agreement_sections (household_id, name, created_at) VALUES ($1, 'Money', now())`, h)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("raw insert err = %v, want a *pgconn.PgError", err)
	}
	if pgErr.ConstraintName != "agreement_sections_household_id_name_key" {
		t.Fatalf("constraint = %q, want agreement_sections_household_id_name_key -- translate matches this string",
			pgErr.ConstraintName)
	}
}

// Decision 17's starter set: four names, one transaction, a second call a
// no-op, and the read-back proving all four landed rather than two of four.
// This household has no proposals or agreements, so it is also where the
// port's "every slice non-nil" is asserted -- the service serialises these
// onto the wire, and Go decodes null and [] identically.
func TestCreateSectionsIsIdempotentAndEverySliceIsNonNil(t *testing.T) {
	ctx := context.Background()
	repo, _, h, _ := newAgreementRepo(t) // "Money" already exists, created at at(0)
	names := []string{"Money", "Conflict", "Home & kids", "Us"}

	firstCall, err := repo.CreateSections(ctx, h, names, at(1))
	if err != nil {
		t.Fatalf("CreateSections: %v", err)
	}
	if len(firstCall) != 4 {
		t.Fatalf("%d sections read back, want 4 -- two of four landing leaves a household half-seeded", len(firstCall))
	}

	secondCall, err := repo.CreateSections(ctx, h, names, at(2))
	if err != nil {
		t.Fatalf("CreateSections again: %v", err)
	}
	firstIDs := make([]string, 0, 4)
	for _, s := range firstCall {
		firstIDs = append(firstIDs, s.ID)
	}
	secondIDs := make([]string, 0, 4)
	for _, s := range secondCall {
		secondIDs = append(secondIDs, s.ID)
	}
	if !slices.Equal(firstIDs, secondIDs) {
		t.Fatalf("second call gave %v, want the same rows %v -- ON CONFLICT DO NOTHING, so a second click is a no-op",
			secondIDs, firstIDs)
	}

	doc, err := repo.Document(ctx, h)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if len(doc.Sections) != 4 {
		t.Fatalf("%d sections in the document, want 4 -- a duplicate name must not create a fifth row", len(doc.Sections))
	}
	// Stamped strictly apart and in the order given, so created_at, id is a
	// total order: "Money" keeps at(0) from newAgreementRepo and "Us" is last.
	if doc.Sections[0].Name != "Money" || doc.Sections[3].Name != "Us" {
		t.Fatalf("sections = %v, want Money first and Us last", doc.Sections)
	}
	if doc.Sections == nil || doc.Agreements == nil || doc.Open == nil || doc.Accepted == nil {
		t.Fatalf("a nil slice reached the port: sections=%v agreements=%v open=%v accepted=%v",
			doc.Sections == nil, doc.Agreements == nil, doc.Open == nil, doc.Accepted == nil)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestAgreementDocument|TestCreateSection' -count=1 -v`

Expected: FAIL, and it is a **build** failure — nothing runs, no `--- FAIL` line is printed:

```
# github.com/andreasoentoro/hearth/api/internal/adapter/postgres_test [github.com/andreasoentoro/hearth/api/internal/adapter/postgres.test]
./agreement_repo_test.go:52:10: undefined: postgres.NewAgreementRepo
FAIL	github.com/andreasoentoro/hearth/api/internal/adapter/postgres [build failed]
```

The `[build failed]` marker is how you tell this from a test failure; say which you saw when you record it in Step 8's commit body.

- [ ] **Step 3: Write the queries**

Create `api/internal/adapter/postgres/queries/agreements.sql`.

```sql
-- Ordering is created_at, id everywhere (decision 11: no position column, so
-- insertion order IS the order). The one exception is
-- ListAcceptedAgreementProposals: a version is the k-th ACCEPTANCE, and two
-- proposals created A then B can be accepted B then A.

-- name: ListAgreementSections :many
SELECT id, name, created_at FROM agreement_sections
WHERE household_id = $1 ORDER BY created_at, id;

-- Live rows only. The predicate is here rather than in Go so a growing tail
-- of removed rows is never fetched to be filtered away, and it matches
-- agreements_household_live_idx.
-- name: ListLiveAgreements :many
SELECT id, section_id, body, added_by_proposal_id, created_at FROM agreements
WHERE household_id = $1 AND removed_at IS NULL ORDER BY created_at, id;

-- The three proposal reads select p.* plus the same signed_by aggregate, so
-- sqlc generates three structs with identical fields in identical order --
-- which is what makes toAgreementProposal's conversion at each call site
-- legal. Change one select list and the conversion stops compiling: that is
-- the warning, not an accident.
--
-- GROUP BY p.id is enough because id is the primary key (Postgres then treats
-- every other column of p as functionally dependent), the same rule
-- ListRetroActions documents. The FILTER clause is what keeps an unsigned
-- proposal at '{}' rather than array_agg's default one-element array holding
-- a single NULL, and the ORDER BY inside array_agg makes the signature list
-- stable between reads.
-- name: ListOpenAgreementProposals :many
SELECT p.*,
       COALESCE(array_agg(s.membership_id ORDER BY s.signed_at, s.membership_id)
                FILTER (WHERE s.membership_id IS NOT NULL), '{}')::uuid[] AS signed_by
FROM agreement_proposals p
LEFT JOIN agreement_signatures s ON s.proposal_id = p.id
WHERE p.household_id = $1 AND p.status IN ('pending', 'parked')
GROUP BY p.id ORDER BY p.created_at, p.id;

-- name: ListAcceptedAgreementProposals :many
SELECT p.*,
       COALESCE(array_agg(s.membership_id ORDER BY s.signed_at, s.membership_id)
                FILTER (WHERE s.membership_id IS NOT NULL), '{}')::uuid[] AS signed_by
FROM agreement_proposals p
LEFT JOIN agreement_signatures s ON s.proposal_id = p.id
WHERE p.household_id = $1 AND p.status = 'accepted'
GROUP BY p.id ORDER BY p.resolved_at, p.id;

-- One proposal whatever its status, withdrawn included: the withdraw
-- handler's proposer check costs one query rather than a composed document.
-- name: GetAgreementProposal :one
SELECT p.*,
       COALESCE(array_agg(s.membership_id ORDER BY s.signed_at, s.membership_id)
                FILTER (WHERE s.membership_id IS NOT NULL), '{}')::uuid[] AS signed_by
FROM agreement_proposals p
LEFT JOIN agreement_signatures s ON s.proposal_id = p.id
WHERE p.household_id = $1 AND p.id = $2
GROUP BY p.id;

-- No ON CONFLICT here: the unique index decides a name collision and
-- translate maps it by constraint name (decision 19). A "does this name
-- exist" pre-read is a check-then-write two owners can both pass.
-- name: CreateAgreementSection :one
INSERT INTO agreement_sections (household_id, name, created_at) VALUES ($1, $2, $3)
RETURNING id, name, created_at;

-- The starter set's insert (decision 17): DO NOTHING, so a second click is a
-- no-op rather than a 409.
-- name: CreateAgreementSectionIfAbsent :exec
INSERT INTO agreement_sections (household_id, name, created_at) VALUES ($1, $2, $3)
ON CONFLICT (household_id, name) DO NOTHING;

-- The starter set's read-back, run inside the same transaction: it is how the
-- caller proves all four landed rather than two of four.
-- name: ListAgreementSectionsNamed :many
SELECT id, name, created_at FROM agreement_sections
WHERE household_id = $1 AND name = ANY(sqlc.arg(names)::text[])
ORDER BY created_at, id;
```

- [ ] **Step 4: Map the unique constraint by name**

In `user_repo.go`, immediately after `billNameUniqueConstraint` (line 198):

```go
// agreementSectionNameUniqueConstraint is the name Postgres gave
// agreement_sections' own UNIQUE (household_id, name)
// (migrations/00014_agreements.sql), the same "<table>_<columns>_key" default
// naming categoryNameUniqueConstraint's own comment explains. translate
// checks this by name, not only by SQLSTATE 23505, so a future unique key on
// the table cannot masquerade as a name collision (decision 19). Sections are
// never deleted, so a name is never freed once taken.
const agreementSectionNameUniqueConstraint = "agreement_sections_household_id_name_key"
```

Then a `translate` case **immediately above** the bare `pgUniqueViolation` arm — line 235 before this step's first edit pushed it down — because `errors.As` matches in source order and the bare arm would otherwise swallow it as the generic `ErrAlreadyExists`:

```go
	case errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == agreementSectionNameUniqueConstraint:
		// AgreementRepository.CreateSection's own contract: the unique index
		// decides the collision, never a pre-read, and the screen has to be
		// able to say "you already have a section called that" rather than
		// showing whatever generic message ALREADY_EXISTS carries.
		return fmt.Errorf("%s: constraint %q: %w", op, pgErr.ConstraintName, domain.ErrAgreementSectionNameTaken)
```

- [ ] **Step 5: Regenerate, then check the generated row structs**

Run `make sqlc` (`cd api && go tool sqlc generate`), then open `api/internal/adapter/postgres/sqlcgen/agreements.sql.go` and confirm that `ListOpenAgreementProposalsRow`, `ListAcceptedAgreementProposalsRow` and `GetAgreementProposalRow` have **identical fields in identical order**:

```
ID, HouseholdID, Kind, Status, SectionID, TargetAgreementID, Body, PreviousBody,
Note, ParkNote, ProposedByMembershipID, CreatedAt, ResolvedAt, SignedBy
```

Step 6's `toAgreementProposal(sqlcgen.GetAgreementProposalRow(p))` is a Go struct conversion and needs exactly that. Nothing else in this repository writes `alias.*` in a select list, so if sqlc has not expanded `p.*`, replace it in all three queries with the explicit column list above minus `SignedBy`, in that order (`p.id, p.household_id, p.kind, …, p.resolved_at`), and run `make sqlc` again.

- [ ] **Step 6: Write the repository**

Create `api/internal/adapter/postgres/agreement_repo.go`.

```go
package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// AgreementRepo keeps the pool alongside the pool-backed *sqlcgen.Queries,
// like VisionRepo, BudgetRepo and GoalRepo: CreateSections here, and
// CreateProposal and Sign in agreement_write_repo.go, each begin their own
// transaction, which a *sqlcgen.Queries built once at construction time
// cannot do.
type AgreementRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

func NewAgreementRepo(db *DB) *AgreementRepo {
	return &AgreementRepo{q: sqlcgen.New(db.Pool()), pool: db.Pool()}
}

// Document is the whole screen in one read: sections, live agreements, open
// proposals and accepted ones. Four queries rather than one join, because the
// four slices are in three different orders and a join would fan every
// signature out across the product. Withdrawn proposals appear in neither
// proposal slice because neither query selects them -- excluded in SQL, so no
// Go-side filter can drift away from the contract.
func (r *AgreementRepo) Document(ctx context.Context, householdID string) (usecase.AgreementDocument, error) {
	h := uuid(householdID)

	sectionRows, err := r.q.ListAgreementSections(ctx, h)
	if err != nil {
		return usecase.AgreementDocument{}, translate(err, "list agreement sections")
	}
	agreementRows, err := r.q.ListLiveAgreements(ctx, h)
	if err != nil {
		return usecase.AgreementDocument{}, translate(err, "list live agreements")
	}
	openRows, err := r.q.ListOpenAgreementProposals(ctx, h)
	if err != nil {
		return usecase.AgreementDocument{}, translate(err, "list open agreement proposals")
	}
	acceptedRows, err := r.q.ListAcceptedAgreementProposals(ctx, h)
	if err != nil {
		return usecase.AgreementDocument{}, translate(err, "list accepted agreement proposals")
	}

	// make(..., 0, n) on all four: the port's contract is every slice non-nil,
	// because the service hands them to the JSON encoder and null is not [].
	doc := usecase.AgreementDocument{
		Sections:   make([]usecase.AgreementSectionRecord, 0, len(sectionRows)),
		Agreements: make([]usecase.AgreementRecord, 0, len(agreementRows)),
		Open:       make([]usecase.AgreementProposalRecord, 0, len(openRows)),
		Accepted:   make([]usecase.AgreementProposalRecord, 0, len(acceptedRows)),
	}
	for _, s := range sectionRows {
		doc.Sections = append(doc.Sections, usecase.AgreementSectionRecord{
			ID: uuidToString(s.ID), Name: s.Name, CreatedAt: timeOf(s.CreatedAt),
		})
	}
	for _, a := range agreementRows {
		doc.Agreements = append(doc.Agreements, usecase.AgreementRecord{
			ID:                uuidToString(a.ID),
			SectionID:         uuidToString(a.SectionID),
			Body:              a.Body,
			AddedByProposalID: uuidToString(a.AddedByProposalID),
			CreatedAt:         timeOf(a.CreatedAt),
		})
	}
	for _, p := range openRows {
		rec, err := toAgreementProposal(sqlcgen.GetAgreementProposalRow(p))
		if err != nil {
			// Returned unwrapped: a kind or status no migration allows is a
			// corrupt row, and it has to reach the caller as itself rather
			// than as "list open agreement proposals: ...".
			return usecase.AgreementDocument{}, err
		}
		doc.Open = append(doc.Open, rec)
	}
	for _, p := range acceptedRows {
		rec, err := toAgreementProposal(sqlcgen.GetAgreementProposalRow(p))
		if err != nil {
			return usecase.AgreementDocument{}, err
		}
		doc.Accepted = append(doc.Accepted, rec)
	}
	return doc, nil
}

// Proposal answers for one proposal whatever its status, withdrawn included.
// That is the whole reason it exists beside Document: a resolved proposal is
// exactly the row Document's two slices leave out, and the withdraw handler
// needs it before it can know whose the proposal is.
func (r *AgreementRepo) Proposal(ctx context.Context, householdID, proposalID string) (usecase.AgreementProposalRecord, error) {
	row, err := r.q.GetAgreementProposal(ctx, sqlcgen.GetAgreementProposalParams{
		HouseholdID: uuid(householdID),
		ID:          uuid(proposalID),
	})
	if err != nil {
		return usecase.AgreementProposalRecord{}, translate(err, "get agreement proposal")
	}
	return toAgreementProposal(row)
}

// CreateSection is immediate and unsigned: a heading is not a promise
// (decision 8). A name collision arrives as ErrAgreementSectionNameTaken
// because translate maps agreement_sections_household_id_name_key by name --
// the unique index decides it, never a "does this name exist" pre-read, which
// is a check-then-write two owners can both pass.
func (r *AgreementRepo) CreateSection(ctx context.Context, householdID, name string, createdAt time.Time) (usecase.AgreementSectionRecord, error) {
	row, err := r.q.CreateAgreementSection(ctx, sqlcgen.CreateAgreementSectionParams{
		HouseholdID: uuid(householdID),
		Name:        name,
		CreatedAt:   timestamptz(createdAt),
	})
	if err != nil {
		return usecase.AgreementSectionRecord{}, translate(err, "create agreement section")
	}
	return usecase.AgreementSectionRecord{
		ID: uuidToString(row.ID), Name: row.Name, CreatedAt: timeOf(row.CreatedAt),
	}, nil
}

// CreateSections seeds decision 17's starter set: every name in ONE
// transaction, ON CONFLICT DO NOTHING so a second click is a no-op rather
// than a 409, then read back inside it -- two of four landing would leave a
// household half-seeded with no button left to ask for the rest.
func (r *AgreementRepo) CreateSections(ctx context.Context, householdID string, names []string, createdAt time.Time) ([]usecase.AgreementSectionRecord, error) {
	out := make([]usecase.AgreementSectionRecord, 0, len(names))
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		for i, name := range names {
			// Stamped strictly apart, never one shared instant: created_at,
			// id is the document's only order, so four rows sharing a
			// timestamp would render in random uuid order (00014_agreements
			// .sql's comment on the deliberately missing DEFAULT now()).
			if err := q.CreateAgreementSectionIfAbsent(ctx, sqlcgen.CreateAgreementSectionIfAbsentParams{
				HouseholdID: uuid(householdID),
				Name:        name,
				CreatedAt:   timestamptz(createdAt.Add(time.Duration(i) * time.Microsecond)),
			}); err != nil {
				return translate(err, "create agreement section if absent")
			}
		}
		rows, err := q.ListAgreementSectionsNamed(ctx, sqlcgen.ListAgreementSectionsNamedParams{
			HouseholdID: uuid(householdID),
			Names:       names,
		})
		if err != nil {
			return translate(err, "list agreement sections named")
		}
		for _, s := range rows {
			out = append(out, usecase.AgreementSectionRecord{
				ID: uuidToString(s.ID), Name: s.Name, CreatedAt: timeOf(s.CreatedAt),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// toAgreementProposal re-parses kind and status rather than casting them: a
// value no migration allowed is a corrupt row, and the parsers refuse it here
// instead of letting it reach a switch upstairs. The conversion at its call
// sites is legal because ListOpen…, ListAccepted… and GetAgreementProposal
// all select p.* plus the same signed_by, so sqlc generates three structs
// with identical fields in identical order.
func toAgreementProposal(row sqlcgen.GetAgreementProposalRow) (usecase.AgreementProposalRecord, error) {
	kind, err := domain.ParseAgreementProposalKind(row.Kind)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	status, err := domain.ParseAgreementProposalStatus(row.Status)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	return usecase.AgreementProposalRecord{
		ID:                     uuidToString(row.ID),
		Kind:                   string(kind),
		Status:                 string(status),
		SectionID:              uuidToString(row.SectionID),
		TargetAgreementID:      optionalIDToString(row.TargetAgreementID),
		Body:                   row.Body,
		PreviousBody:           row.PreviousBody,
		Note:                   row.Note,
		ParkNote:               row.ParkNote,
		ProposedByMembershipID: uuidToString(row.ProposedByMembershipID),
		CreatedAt:              timeOf(row.CreatedAt),
		ResolvedAt:             timePtrOf(row.ResolvedAt),
		SignedByMembershipIDs:  membershipIDs(row.SignedBy),
	}, nil
}

// membershipIDs is assigneeIDs (retro_action_repo.go:220) with the one
// difference this port needs: an unsigned proposal gets [], never nil.
// AwaitingSignature ranges over this list, and a nil slice would be one more
// shape for the service to think about for no benefit.
func membershipIDs(ids []pgtype.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, uuidToString(id))
	}
	return out
}
```

- [ ] **Step 7: Run the tests and watch them pass**

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestAgreementDocument|TestCreateSection' -count=1 -v`

Expected: PASS, all four (each boots its own container, so budget roughly fifteen seconds).

- [ ] **Step 8: Mutation-check the accepted ordering**

In `ListAcceptedAgreementProposals`, change the last line to `GROUP BY p.id ORDER BY p.created_at, p.id;`, run `make sqlc`, rerun the command from Step 7.

Expected: a **test** failure, not a build failure — the generated function's signature does not change, so the package still compiles:

```
--- FAIL: TestAgreementDocumentOrdersOpenByCreatedAtAndAcceptedByResolvedAt (3.42s)
    agreement_repo_test.go:146: accepted = [<D's id> <E's id>], want <E's id> then <D's id> -- ordered by resolved_at, not created_at
```

D was created first and accepted last, so a created_at ordering puts it first. The other three tests stay green: none of them has two accepted proposals. Restore the line and run `make sqlc` again.

- [ ] **Step 9: Commit**

```bash
cd api && go test ./internal/adapter/postgres/ -count=1 && cd .. && make lint
git add api/internal/adapter/postgres/queries/agreements.sql \
        api/internal/adapter/postgres/agreement_repo.go \
        api/internal/adapter/postgres/agreement_repo_test.go \
        api/internal/adapter/postgres/user_repo.go \
        api/internal/adapter/postgres/sqlcgen
git commit -m "feat(agreements): the document read, and a section name that says what collided

Four list queries, Document/Proposal/CreateSection/CreateSections on
AgreementRepo, and agreement_sections_household_id_name_key mapped by
constraint name above the bare 23505 arm so the screen can say which name
collided.

Mutation check: ListAcceptedAgreementProposals' ORDER BY p.resolved_at, p.id
-> ORDER BY p.created_at, p.id, then make sqlc. Red:
TestAgreementDocumentOrdersOpenByCreatedAtAndAcceptedByResolvedAt on its
'accepted = ...' assertion. What I saw was a TEST failure, not a build
failure -- the regenerated function keeps its signature, so the package still
compiled."
```

---

### Task 6: The transactional writes

**Files:**
- Create: `api/internal/adapter/postgres/agreement_write_repo.go`, `api/internal/adapter/postgres/agreement_write_repo_test.go`
- Modify: `api/internal/adapter/postgres/queries/agreements.sql` (append), `api/internal/adapter/postgres/convert.go` (one line in the compile-time port assertions), `api/internal/adapter/postgres/sqlcgen/**` (regenerated)

**Interfaces:**

- Consumes, from Task 5: `*AgreementRepo` with its `q` and `pool` fields, `toAgreementProposal`, `Proposal`, `Document`, and the test helpers `at`, `newAgreementRepo`, `insertTestProposal`, `insertTestAgreement`, `insertTestSignature`.
- Consumes, from Task 2:

```go
const MinAgreementOwners = 2
const (
    ProposalAdd    AgreementProposalKind = "add"
    ProposalEdit   AgreementProposalKind = "edit"
    ProposalRemove AgreementProposalKind = "remove"
)
func ParseAgreementProposalKind(s string) (AgreementProposalKind, error)
func ParseAgreementProposalStatus(s string) (AgreementProposalStatus, error)
func (s AgreementProposalStatus) IsOpen() bool
var ErrAgreementChanged, ErrAgreementNotOpen, ErrUnknownAgreementProposalKind error
```

- Consumes, from Task 3 (copy verbatim):

```go
type AgreementProposalWrite struct {
    HouseholdID, Kind, SectionID, TargetAgreementID, Body, PreviousBody, Note,
    ProposedByMembershipID string
    CreatedAt              time.Time
}
type AgreementSignatureWrite struct {
    HouseholdID, ProposalID, MembershipID string
    At                                    time.Time
}
```

- Produces, completing `usecase.AgreementRepository` on the same `*AgreementRepo`:

```go
func (r *AgreementRepo) CreateProposal(ctx context.Context, in usecase.AgreementProposalWrite) (usecase.AgreementProposalRecord, error)
func (r *AgreementRepo) Sign(ctx context.Context, in usecase.AgreementSignatureWrite) (usecase.AgreementProposalRecord, error)
func (r *AgreementRepo) Park(ctx context.Context, householdID, proposalID, note string, at time.Time) (usecase.AgreementProposalRecord, error)
func (r *AgreementRepo) Withdraw(ctx context.Context, householdID, proposalID, byMembershipID string, at time.Time) (usecase.AgreementProposalRecord, error)
```

  and, in `convert.go`'s existing compile-time block, the line that makes a future drift from `ports.go` a compile error here rather than in Task 7's wiring:

```go
	_ usecase.AgreementRepository = (*AgreementRepo)(nil)
```

- [ ] **Step 1: Write the failing tests**

Create `api/internal/adapter/postgres/agreement_write_repo_test.go`.

```go
package postgres_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// agreementFixture is the two-owner household every write test starts from:
// Alex and Casey (insertTestMembership creates owners), one "Money" section,
// and the repository under test. A test needing a third owner adds one the
// same way -- the signing set is every CURRENT owner (decision 4), so a third
// owner is a third signature.
type agreementFixture struct {
	repo    *postgres.AgreementRepo
	db      *postgres.DB
	h       string
	section string
	alex    string
	casey   string
}

func newAgreementFixture(t *testing.T) agreementFixture {
	t.Helper()
	repo, db, h, section := newAgreementRepo(t)
	return agreementFixture{
		repo:    repo,
		db:      db,
		h:       h,
		section: section,
		alex:    insertTestMembership(t, db, h, "Alex"),
		casey:   insertTestMembership(t, db, h, "Casey"),
	}
}

// propose files one add from Alex, which also records Alex's own signature in
// the same transaction (decision 5).
func (f agreementFixture) propose(t *testing.T, body string) usecase.AgreementProposalRecord {
	t.Helper()
	p, err := f.repo.CreateProposal(context.Background(), usecase.AgreementProposalWrite{
		HouseholdID:            f.h,
		Kind:                   "add",
		SectionID:              f.section,
		Body:                   body,
		ProposedByMembershipID: f.alex,
		CreatedAt:              at(1),
	})
	if err != nil {
		t.Fatalf("CreateProposal(%q): %v", body, err)
	}
	return p
}

// landAdd proposes and then signs as Casey, which completes a two-owner
// signing set, and returns the one live agreement that produced.
func (f agreementFixture) landAdd(t *testing.T, body string) usecase.AgreementRecord {
	t.Helper()
	ctx := context.Background()
	p := f.propose(t, body)
	if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: f.casey, At: at(2),
	}); err != nil {
		t.Fatalf("Sign(%q): %v", body, err)
	}
	doc, err := f.repo.Document(ctx, f.h)
	if err != nil {
		t.Fatalf("Document: %v", err)
	}
	if len(doc.Agreements) != 1 {
		t.Fatalf("%d live agreements after landing %q, want exactly 1", len(doc.Agreements), body)
	}
	return doc.Agreements[0]
}

// countRow reads one integer on the POOL rather than through the repository:
// a count that went through the code under test could be wrong in the same
// direction as the bug it is meant to catch.
func countRow(t *testing.T, db *postgres.DB, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count (%s): %v", sql, err)
	}
	return n
}

// execSQL runs one raw statement on the pool, for the fixtures and the
// deliberate spoils the repository has no method for.
func execSQL(t *testing.T, db *postgres.DB, sql string, args ...any) {
	t.Helper()
	if _, err := db.Pool().Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec (%s): %v", sql, err)
	}
}

// heldConns is bill_repo_test.go:525-536's inline loop lifted into a function,
// because this file makes the same assertion three times. Polled rather than
// sampled once: pgxpool runs a background health check on a 500ms timer that
// briefly acquires an idle connection, so a single sample can catch an
// unrelated blip, while a LEAKED connection never comes back -- requiring the
// count to reach zero within a second tells the two apart without weakening
// the claim. bill_repo_test.go keeps its own copy: rewiring a passing test in
// an unrelated file is not this task's work.
func heldConns(db *postgres.DB) int32 {
	deadline := time.Now().Add(time.Second)
	for {
		held := db.Pool().Stat().AcquiredConns()
		if held == 0 || time.Now().After(deadline) {
			return held
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The fault has to be on the SECOND write or there is nothing for a partial
// write to leak. A proposer who is not an owner here makes the signature
// INSERT match zero rows, and the proposal written a statement earlier must
// go back with it.
//
// The spec words this as "against a section in another household"; for
// CreateProposal that would fail on InsertAgreementProposal, the FIRST
// statement, leaving nothing written to leak -- so the fault used here is the
// proposer instead. Sign, below, is where the foreign-section fault lands on
// a late statement.
func TestCreateProposalWritesNothingWhenTheProposerIsNotAnOwnerHere(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	stranger := insertTestMembership(t, f.db, insertTestHousehold(t, f.db), "Stranger")

	_, err := f.repo.CreateProposal(ctx, usecase.AgreementProposalWrite{
		HouseholdID:            f.h,
		Kind:                   "add",
		SectionID:              f.section,
		Body:                   "we save 20%",
		ProposedByMembershipID: stranger,
		CreatedAt:              at(1),
	})
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_proposals WHERE household_id = $1`, f.h); n != 0 {
		t.Fatalf("%d proposals survived a refused signature, want 0", n)
	}
	if held := heldConns(f.db); held != 0 {
		t.Fatalf("%d connection(s) still checked out -- the transaction was never rolled back", held)
	}
}

// The propose-time half of decision 13. CreateProposal makes the same
// comparison Sign's step 2 makes, in its own transaction against its own row:
// without it a stale proposer files an edit nobody can ever sign, and their
// partner is told THEY are the one out of date.
func TestCreateProposalRefusesAStaleTarget(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	target := f.landAdd(t, "we save 20%")

	_, err := f.repo.CreateProposal(ctx, usecase.AgreementProposalWrite{
		HouseholdID:            f.h,
		Kind:                   "edit",
		TargetAgreementID:      target.ID,
		Body:                   "we save 25%",
		PreviousBody:           "we save 15%", // never the stored wording
		ProposedByMembershipID: f.alex,
		CreatedAt:              at(3),
	})
	if !errors.Is(err, domain.ErrAgreementChanged) {
		t.Fatalf("err = %v, want ErrAgreementChanged", err)
	}
	// One proposal in the household: the accepted add that landed the
	// target, and nothing this call wrote.
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_proposals WHERE household_id = $1`, f.h); n != 1 {
		t.Fatalf("%d proposals, want 1 -- the refused propose wrote nothing", n)
	}
}

// The two ids arrive from a request body, and uuid() folds an unparseable one
// into the same zero UUID an ABSENT one produces (convert.go's uuidLooksValid
// comment). The guard is per kind, and the two kinds answer differently: an
// unknown section is ErrNotFound, an unknown target is ErrAgreementChanged --
// to this household that agreement is indistinguishable from one that has
// been removed, and both mean the same thing to the caller.
func TestCreateProposalRefusesMalformedIDsPerKind(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)

	if _, err := f.repo.CreateProposal(ctx, usecase.AgreementProposalWrite{
		HouseholdID: f.h, Kind: "add", SectionID: "banana", Body: "we save 20%",
		ProposedByMembershipID: f.alex, CreatedAt: at(1),
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("add with an unparseable section: err = %v, want ErrNotFound", err)
	}
	if _, err := f.repo.CreateProposal(ctx, usecase.AgreementProposalWrite{
		HouseholdID: f.h, Kind: "remove", TargetAgreementID: "banana",
		PreviousBody: "we save 20%", ProposedByMembershipID: f.alex, CreatedAt: at(1),
	}); !errors.Is(err, domain.ErrAgreementChanged) {
		t.Fatalf("remove with an unparseable target: err = %v, want ErrAgreementChanged", err)
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_proposals WHERE household_id = $1`, f.h); n != 0 {
		t.Fatalf("%d proposals, want 0 -- both refusals happen before the transaction opens", n)
	}
}

// Sign's fault is also on a late write: the apply INSERT is scoped through
// agreement_sections, so a proposal repointed at another household's section
// fails there, after the signature has already been written. Nine of the ten
// pool connections (pool.go:23) are held for the duration, which is what
// proves EVERY statement of Sign runs on the transaction's own connection --
// a pool-backed call inside pgx.BeginFunc would block on a connection nothing
// can release. The VisionRepo.Save hang, asserted rather than hoped for.
func TestSignIsOneTransactionOnItsOwnConnection(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	p := f.propose(t, "we save 20%")

	var foreign string
	if err := f.db.Pool().QueryRow(ctx,
		`INSERT INTO agreement_sections (household_id, name, created_at)
		 VALUES ($1, 'Money', now()) RETURNING id`, insertTestHousehold(t, f.db)).Scan(&foreign); err != nil {
		t.Fatalf("insert foreign section: %v", err)
	}
	execSQL(t, f.db, `UPDATE agreement_proposals SET section_id = $1 WHERE id = $2`, foreign, p.ID)

	var hold []*pgxpool.Conn
	for i := 0; i < 9; i++ {
		c, err := f.db.Pool().Acquire(ctx)
		if err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
		hold = append(hold, c)
	}
	signCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := f.repo.Sign(signCtx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: f.casey, At: at(2),
	})
	for _, c := range hold {
		c.Release()
	}

	// The positive assertion, not "no timeout": a wrapped deadline that
	// errors.Is does not unwrap would slip past a negative check, and the
	// rollback would leave exactly the counts below either way.
	if !errors.Is(err, domain.ErrAgreementChanged) {
		t.Fatalf("err = %v, want ErrAgreementChanged -- a deadline here means a statement reached back to the pool", err)
	}
	sigs := countRow(t, f.db, `SELECT count(*) FROM agreement_signatures WHERE proposal_id = $1`, p.ID)
	agreements := countRow(t, f.db, `SELECT count(*) FROM agreements WHERE household_id = $1`, f.h)
	if sigs != 1 || agreements != 0 {
		t.Fatalf("signatures = %d (want 1, the proposer's), agreements = %d (want 0)", sigs, agreements)
	}
	if held := heldConns(f.db); held != 0 {
		t.Fatalf("%d connection(s) still checked out, want 0", held)
	}
}

// One subtest per predicate of Sign's step 2 SELECT ... FOR UPDATE, so a
// mutation cannot go red on the wrong one. All three answer
// ErrAgreementChanged: "it vanished" and "someone changed it" are the same
// thing to the caller (decision 13), and ErrNotFound would be false -- the
// proposal WAS found, only its target moved.
func TestSignRefusesAChangedTarget(t *testing.T) {
	cases := []struct {
		name  string
		spoil func(t *testing.T, f agreementFixture, targetID string)
	}{
		{"target_in_another_household", func(t *testing.T, f agreementFixture, targetID string) {
			execSQL(t, f.db, `UPDATE agreements SET household_id = $2 WHERE id = $1`,
				targetID, insertTestHousehold(t, f.db))
		}},
		{"target_removed", func(t *testing.T, f agreementFixture, targetID string) {
			execSQL(t, f.db, `UPDATE agreements
				 SET removed_at = now(), removed_by_proposal_id = added_by_proposal_id
				 WHERE id = $1`, targetID)
		}},
		{"target_reworded", func(t *testing.T, f agreementFixture, targetID string) {
			execSQL(t, f.db, `UPDATE agreements SET body = 'we save 30%' WHERE id = $1`, targetID)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newAgreementFixture(t) // its own household: a spoiled row must not leak sideways
			target := f.landAdd(t, "we save 20%")
			edit, err := f.repo.CreateProposal(ctx, usecase.AgreementProposalWrite{
				HouseholdID:            f.h,
				Kind:                   "edit",
				TargetAgreementID:      target.ID,
				Body:                   "we save 25%",
				PreviousBody:           "we save 20%",
				ProposedByMembershipID: f.alex,
				CreatedAt:              at(3),
			})
			if err != nil {
				t.Fatalf("CreateProposal(edit): %v", err)
			}
			tc.spoil(t, f, target.ID)

			if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
				HouseholdID: f.h, ProposalID: edit.ID, MembershipID: f.casey, At: at(4),
			}); !errors.Is(err, domain.ErrAgreementChanged) {
				t.Fatalf("err = %v, want ErrAgreementChanged", err)
			}
			// The refusal wrote nothing: the proposal is untouched and
			// Casey's signature never landed.
			still, err := f.repo.Proposal(ctx, f.h, edit.ID)
			if err != nil {
				t.Fatalf("Proposal: %v", err)
			}
			if still.Status != "pending" {
				t.Fatalf("status = %q, want pending -- a refused sign applies nothing", still.Status)
			}
			if n := countRow(t, f.db, `SELECT count(*) FROM agreement_signatures WHERE proposal_id = $1`, edit.ID); n != 1 {
				t.Fatalf("%d signatures on the refused edit, want 1 -- only the proposer's", n)
			}
		})
	}
}

// Three owners (decision 4, counted live and in-transaction): the second
// signature commits with the status unchanged, a repeat of it keeps the FIRST
// signed_at -- asserted by value, not by a row count -- and only the third
// completes the change, writing exactly one agreements row. The last signer
// pressing Agree twice is refused and writes no second row.
func TestSignCompletesOnlyWhenEveryCurrentOwnerHasSigned(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	drew := insertTestMembership(t, f.db, f.h, "Drew")
	p := f.propose(t, "we save 20%") // Alex's own signature is the first

	second, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: f.casey, At: at(2),
	})
	if err != nil {
		t.Fatalf("Sign as Casey: %v", err)
	}
	if second.Status != "pending" || second.ResolvedAt != nil {
		t.Fatalf("status = %q resolvedAt = %v after 2 of 3 signatures, want pending and nil",
			second.Status, second.ResolvedAt)
	}

	// A repeat Agree is idempotent and keeps the first signed_at (decision
	// 16). Read out of the column, because a row count stays 1 whether the
	// upsert left signed_at alone or overwrote it.
	if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: f.casey, At: at(5),
	}); err != nil {
		t.Fatalf("repeat Sign as Casey: %v", err)
	}
	var signedAt time.Time
	if err := f.db.Pool().QueryRow(ctx,
		`SELECT signed_at FROM agreement_signatures WHERE proposal_id = $1 AND membership_id = $2`,
		p.ID, f.casey).Scan(&signedAt); err != nil {
		t.Fatalf("read signed_at: %v", err)
	}
	if !signedAt.Equal(at(2)) {
		t.Fatalf("signed_at = %s, want the first stamp %s -- DO UPDATE must leave it alone", signedAt, at(2))
	}

	// Not an owner HERE: the signature insert selects through memberships.
	stranger := insertTestMembership(t, f.db, insertTestHousehold(t, f.db), "Stranger")
	if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: stranger, At: at(6),
	}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}

	done, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: drew, At: at(7),
	})
	if err != nil {
		t.Fatalf("Sign as Drew: %v", err)
	}
	if done.Status != "accepted" || done.ResolvedAt == nil || !done.ResolvedAt.Equal(at(7)) {
		t.Fatalf("status = %q resolvedAt = %v, want accepted at %s", done.Status, done.ResolvedAt, at(7))
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreements WHERE household_id = $1`, f.h); n != 1 {
		t.Fatalf("%d agreements, want exactly 1", n)
	}

	// The last signer double-clicking Agree: refused, and the agreement row
	// is written once.
	if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: drew, At: at(8),
	}); !errors.Is(err, domain.ErrAgreementNotOpen) {
		t.Fatalf("repeat agree err = %v, want ErrAgreementNotOpen", err)
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreements WHERE household_id = $1`, f.h); n != 1 {
		t.Fatalf("%d agreements after a repeat agree, want still 1", n)
	}
}

// Three owners on purpose. The mutation this test exists for -- deleting the
// status read that precedes the signature upsert -- is invisible in a
// two-owner fixture: there every non-proposer signature is the completing
// one, so the apply step's own guarded UPDATE errors and the rollback hides
// the extra signature. With three owners the signature is non-completing, so
// without the status read Sign COMMITS a signature on a withdrawn proposal
// and returns no error at all, which is what the count below catches.
func TestSignOnAWithdrawnProposalIsRefusedAndWritesNothing(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	insertTestMembership(t, f.db, f.h, "Drew")
	p := f.propose(t, "we save 20%")
	if _, err := f.repo.Withdraw(ctx, f.h, p.ID, f.alex, at(3)); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	if _, err := f.repo.Sign(ctx, usecase.AgreementSignatureWrite{
		HouseholdID: f.h, ProposalID: p.ID, MembershipID: f.casey, At: at(4),
	}); !errors.Is(err, domain.ErrAgreementNotOpen) {
		t.Fatalf("err = %v, want ErrAgreementNotOpen", err)
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_signatures WHERE proposal_id = $1`, p.ID); n != 1 {
		t.Fatalf("%d signatures, want 1 -- only the proposer's; the refused sign wrote nothing", n)
	}
}

// The log read back field by field, never count(*): park keeps the proposal
// open with its note through the write path, re-parking replaces the note,
// and withdraw closes it -- after which park is refused, and an id naming no
// row at all is ErrNotFound. Those last two are diagnoseGuardedUpdate's first
// two legs; its third is the test below.
func TestParkThenWithdrawReadBackFieldByField(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	p := f.propose(t, "we cook on Sundays")

	parked, err := f.repo.Park(ctx, f.h, p.ID, "let us talk on Sunday", at(2))
	if err != nil {
		t.Fatalf("Park: %v", err)
	}
	if parked.Status != "parked" || parked.ParkNote != "let us talk on Sunday" || parked.ResolvedAt != nil {
		t.Fatalf("park gave status %q note %q resolvedAt %v, want parked / that note / nil -- parking keeps it open",
			parked.Status, parked.ParkNote, parked.ResolvedAt)
	}
	reparked, err := f.repo.Park(ctx, f.h, p.ID, "after the holiday", at(3))
	if err != nil {
		t.Fatalf("Park again: %v", err)
	}
	if reparked.ParkNote != "after the holiday" {
		t.Fatalf("park note = %q, want the second one -- re-parking replaces it", reparked.ParkNote)
	}

	// A parked proposal is still open, so it can still be withdrawn.
	gone, err := f.repo.Withdraw(ctx, f.h, p.ID, f.alex, at(4))
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if gone.Status != "withdrawn" || gone.ResolvedAt == nil || !gone.ResolvedAt.Equal(at(4)) {
		t.Fatalf("withdraw gave status %q resolvedAt %v, want withdrawn at %s", gone.Status, gone.ResolvedAt, at(4))
	}

	if _, err := f.repo.Park(ctx, f.h, p.ID, "one more thought", at(5)); !errors.Is(err, domain.ErrAgreementNotOpen) {
		t.Fatalf("park after withdraw err = %v, want ErrAgreementNotOpen", err)
	}
	if _, err := f.repo.Park(ctx, f.h, uuid.NewString(), "", at(5)); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("park on an unknown id err = %v, want ErrNotFound", err)
	}
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_signatures WHERE proposal_id = $1`, p.ID); n != 1 {
		t.Fatalf("%d signatures, want 1 -- park and withdraw touch no signature", n)
	}
}

// Decision 15 in both directions -- a comparison with its sides swapped
// passes a one-legged test. Then decision 20: deleting the signer's
// membership ROW directly, not the household whose cascade removes both sides
// and proves nothing, must leave the proposal, its signatures and the
// agreement in place.
func TestWithdrawIsTheProposersUntilTheProposerLeavesAndTheRowsOutliveThem(t *testing.T) {
	ctx := context.Background()
	f := newAgreementFixture(t)
	landed := f.landAdd(t, "we save 20%")   // 1 accepted proposal, 2 signatures, 1 agreement
	p := f.propose(t, "we cook on Sundays") // 1 pending proposal, 1 signature

	if _, err := f.repo.Withdraw(ctx, f.h, p.ID, f.casey, at(3)); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden -- withdraw is the proposer's while they are still an owner", err)
	}
	execSQL(t, f.db, `DELETE FROM memberships WHERE id = $1`, f.alex)

	gone, err := f.repo.Withdraw(ctx, f.h, p.ID, f.casey, at(4))
	if err != nil {
		t.Fatalf("Withdraw after the proposer left: %v", err)
	}
	if gone.Status != "withdrawn" {
		t.Fatalf("status = %q, want withdrawn -- once the proposer is no longer an owner, any owner may withdraw",
			gone.Status)
	}

	// Neither cascaded nor refused: the two membership columns are log
	// columns and carry no ON DELETE action at all.
	if n := countRow(t, f.db, `SELECT count(*) FROM agreement_proposals WHERE household_id = $1`, f.h); n != 2 {
		t.Fatalf("%d proposals survived the membership delete, want 2", n)
	}
	if n := countRow(t, f.db,
		`SELECT count(*) FROM agreement_signatures s
		 JOIN agreement_proposals p ON p.id = s.proposal_id
		 WHERE p.household_id = $1`, f.h); n != 3 {
		t.Fatalf("%d signatures survived, want 3 -- two on the accepted add, one on the withdrawn proposal", n)
	}
	if n := countRow(t, f.db,
		`SELECT count(*) FROM agreements WHERE id = $1 AND removed_at IS NULL`, landed.ID); n != 1 {
		t.Fatalf("the agreement Alex proposed is gone (%d live rows), want it still there", n)
	}
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestCreateProposal|TestSign|TestPark|TestWithdraw' -count=1 -v`

Expected: FAIL, and again a **build** failure — one line per missing method, the first being:

```
./agreement_write_repo_test.go:78:24: f.repo.CreateProposal undefined (type *postgres.AgreementRepo has no field or method CreateProposal)
FAIL	github.com/andreasoentoro/hearth/api/internal/adapter/postgres [build failed]
```

Note which you saw; Step 9's commit body has to say.

- [ ] **Step 3: Append the queries**

Append to `api/internal/adapter/postgres/queries/agreements.sql`:

```sql
-- Sign's step 1, lean because FOR UPDATE cannot sit on the GROUP BY/array_agg
-- read above. It also orders two owners pressing Agree on the SAME proposal
-- at the same instant: the second waits here rather than racing.
-- name: LockAgreementProposal :one
SELECT id, kind, status, section_id, target_agreement_id, body, previous_body
FROM agreement_proposals WHERE household_id = $1 AND id = $2 FOR UPDATE;

-- Sign's step 2, and the lock that matters (decision 12): the proposal lock
-- orders two signatures on one proposal and nothing else, so two proposals
-- against the same agreement never contend on it. All three predicates are IN
-- the WHERE -- under READ COMMITTED a waiter re-evaluates them after the
-- holder commits, so a removal or a rewording by the other proposal returns
-- zero rows here. A bare FOR UPDATE plus a Go-side compare defeats exactly
-- that race. CreateProposal makes the same call at propose time.
-- name: LockAgreementTarget :one
SELECT id, section_id FROM agreements
WHERE household_id = $1 AND id = $2 AND removed_at IS NULL AND body = $3 FOR UPDATE;

-- INSERT ... SELECT is the household scoping: a section in another household
-- matches no row, which is indistinguishable from one that does not exist.
-- name: InsertAgreementProposal :one
INSERT INTO agreement_proposals (household_id, kind, status, section_id, target_agreement_id,
    body, previous_body, note, proposed_by_membership_id, created_at)
SELECT sqlc.arg(household_id), sqlc.arg(kind)::text, 'pending', s.id,
       sqlc.narg(target_agreement_id)::uuid, sqlc.arg(body)::text, sqlc.arg(previous_body)::text,
       sqlc.arg(note)::text, sqlc.arg(proposed_by)::uuid, sqlc.arg(created_at)::timestamptz
FROM agreement_sections s
WHERE s.id = sqlc.arg(section_id) AND s.household_id = sqlc.arg(household_id)
RETURNING id;

-- DO UPDATE, never DO NOTHING: it stores nothing new -- signed_at is left
-- alone, keeping the first stamp (decision 16) -- but still counts a row,
-- which is what lets :execrows tell a double-click (1) from a caller who is
-- not an owner here (0). DO NOTHING would make the two identical.
-- name: SignAgreementProposal :execrows
INSERT INTO agreement_signatures (proposal_id, membership_id, signed_at)
SELECT sqlc.arg(proposal_id), m.id, sqlc.arg(signed_at)::timestamptz
FROM memberships m
WHERE m.id = sqlc.arg(membership_id) AND m.household_id = sqlc.arg(household_id) AND m.role = 'owner'
ON CONFLICT (proposal_id, membership_id) DO UPDATE SET membership_id = excluded.membership_id;

-- name: CountAgreementOwners :one
SELECT count(*) FROM memberships WHERE household_id = $1 AND role = 'owner';

-- Joined through memberships: a departed owner's signature is ignored, never
-- deleted (decision 4).
-- name: CountAgreementOwnerSignatures :one
SELECT count(*) FROM agreement_signatures s JOIN memberships m ON m.id = s.membership_id
WHERE s.proposal_id = $1 AND m.household_id = $2 AND m.role = 'owner';

-- Scoped through agreement_sections: the FK alone only proves the section
-- exists SOMEWHERE, and this is where a repointed proposal is caught.
-- name: InsertAgreementFromProposal :one
INSERT INTO agreements (household_id, section_id, body, added_by_proposal_id, created_at)
SELECT sqlc.arg(household_id), s.id, sqlc.arg(body)::text, sqlc.arg(proposal_id),
       sqlc.arg(created_at)::timestamptz
FROM agreement_sections s
WHERE s.id = sqlc.arg(section_id) AND s.household_id = sqlc.arg(household_id)
RETURNING id;

-- AND removed_at IS NULL, so a row already removed is not stamped twice.
-- name: RemoveAgreement :execrows
UPDATE agreements SET removed_at = sqlc.arg(at)::timestamptz, removed_by_proposal_id = sqlc.arg(proposal_id)
WHERE household_id = sqlc.arg(household_id) AND id = sqlc.arg(id) AND removed_at IS NULL;

-- name: AcceptAgreementProposal :execrows
UPDATE agreement_proposals SET status = 'accepted', resolved_at = sqlc.arg(at)::timestamptz
WHERE household_id = sqlc.arg(household_id) AND id = sqlc.arg(id) AND status IN ('pending', 'parked');

-- resolved_at stays NULL: parking keeps the proposal OPEN (decision 7). The
-- status condition is in the WHERE, never a service if -- a check-then-write
-- races. Nothing here touches a retro table and there is no foreign key to a
-- retro row: the next retro usually does not exist yet, which is exactly when
-- a couple parks something.
-- name: ParkAgreementProposal :execrows
UPDATE agreement_proposals SET status = 'parked', park_note = sqlc.arg(note)::text
WHERE household_id = sqlc.arg(household_id) AND id = sqlc.arg(id) AND status IN ('pending', 'parked');

-- The proposer clause is a BACKSTOP; the handler answers first (decision 22).
-- Its second leg is decision 15: once the proposer is no longer an owner
-- here, any owner may withdraw -- without it a proposal a departed partner
-- left behind could never be removed by anyone.
-- name: WithdrawAgreementProposal :execrows
UPDATE agreement_proposals SET status = 'withdrawn', resolved_at = sqlc.arg(at)::timestamptz
WHERE household_id = sqlc.arg(household_id) AND id = sqlc.arg(id)
  AND status IN ('pending', 'parked')
  AND (proposed_by_membership_id = sqlc.arg(by)
       OR NOT EXISTS (SELECT 1 FROM memberships m
                      WHERE m.id = agreement_proposals.proposed_by_membership_id
                        AND m.household_id = sqlc.arg(household_id) AND m.role = 'owner'));
```

- [ ] **Step 4: Regenerate and check the inferred parameter types**

Run `make sqlc`, then open `api/internal/adapter/postgres/sqlcgen/agreements.sql.go` and read the two `INSERT … SELECT` Params structs. Every field must have a concrete type (`pgtype.UUID`, `string`, `pgtype.Timestamptz`); an `interface{}` means sqlc could not infer that select-list expression — add the missing `::uuid` / `::text` / `::timestamptz` cast and run `make sqlc` again. `sqlc.narg(target_agreement_id)::uuid` stays `pgtype.UUID`, not `*string`: `emit_pointers_for_null_types` only wraps scalar columns, since pgtype.UUID already carries its own `Valid` (vision_repo.go's `toMeasure` comment).

- [ ] **Step 5: Write `Sign` and its apply step**

Create `api/internal/adapter/postgres/agreement_write_repo.go` with these two first; the rest of the file arrives in Step 6.

```go
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/andreasoentoro/hearth/api/internal/adapter/postgres/sqlcgen"
	"github.com/andreasoentoro/hearth/api/internal/domain"
	"github.com/andreasoentoro/hearth/api/internal/usecase"
)

// Sign records one Agree and, when that completes the signing set, applies
// the change -- all in one transaction, on that transaction's OWN connection.
// Every statement below runs on q, never r.q: a pool-backed call inside
// pgx.BeginFunc takes a second connection while the first is still held, and
// enough concurrent signers then deadlock against pool.go's MaxConns -- the
// hang VisionRepo.Save shipped. It is the only method that writes an
// agreements row, and it returns the proposal as it then stands.
func (r *AgreementRepo) Sign(ctx context.Context, in usecase.AgreementSignatureWrite) (usecase.AgreementProposalRecord, error) {
	var out usecase.AgreementProposalRecord
	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		h, id := uuid(in.HouseholdID), uuid(in.ProposalID)

		// 1. Lock the proposal. This is also what orders two owners pressing
		// Agree on this proposal at the same instant: the second waits here,
		// and wakes either to 'accepted' (refused below) or to 'pending',
		// where it completes the change itself.
		p, err := q.LockAgreementProposal(ctx, sqlcgen.LockAgreementProposalParams{HouseholdID: h, ID: id})
		if err != nil {
			return translate(err, "lock agreement proposal")
		}
		status, err := domain.ParseAgreementProposalStatus(p.Status)
		if err != nil {
			return err // a value no migration allowed: a corrupt row, not a caller's typo
		}
		if !status.IsOpen() {
			return domain.ErrAgreementNotOpen
		}
		kind, err := domain.ParseAgreementProposalKind(p.Kind)
		if err != nil {
			return err
		}

		// 2. On EVERY signing and BEFORE the signature lands -- after step 4
		// a middle signer's agreement would be recorded against wording that
		// has already moved. The only place in Sign that compares
		// previous_body, and the section an edit or a remove applies to is
		// the TARGET's, read here rather than trusted from the proposal row.
		sectionID := p.SectionID
		if kind != domain.ProposalAdd {
			target, err := q.LockAgreementTarget(ctx, sqlcgen.LockAgreementTargetParams{
				HouseholdID: h, ID: p.TargetAgreementID, Body: p.PreviousBody,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrAgreementChanged
			}
			if err != nil {
				return translate(err, "lock agreement target")
			}
			sectionID = target.SectionID
		}

		// 3. The signature. Zero rows means the caller is not an owner here
		// -- the backstop behind requireOwner.
		n, err := q.SignAgreementProposal(ctx, sqlcgen.SignAgreementProposalParams{
			ProposalID:   id,
			MembershipID: uuid(in.MembershipID),
			HouseholdID:  h,
			SignedAt:     timestamptz(in.At),
		})
		if err != nil {
			return translate(err, "sign agreement proposal")
		}
		if n == 0 {
			return domain.ErrForbidden
		}

		// 4. Both counts in this transaction -- a service-level count cannot
		// close the window (decision 4) -- and owners >= MinAgreementOwners,
		// so a household down to one owner cannot finish a change nobody is
		// left to agree with. Not complete: fall through and commit with the
		// status unchanged.
		owners, err := q.CountAgreementOwners(ctx, h)
		if err != nil {
			return translate(err, "count agreement owners")
		}
		signed, err := q.CountAgreementOwnerSignatures(ctx, sqlcgen.CountAgreementOwnerSignaturesParams{
			ProposalID: id, HouseholdID: h,
		})
		if err != nil {
			return translate(err, "count agreement owner signatures")
		}
		if owners >= int64(domain.MinAgreementOwners) && signed == owners {
			if err := applyAgreementChange(ctx, q, h, id, sectionID, kind, p, in.At); err != nil {
				return err
			}
		}

		row, err := q.GetAgreementProposal(ctx, sqlcgen.GetAgreementProposalParams{HouseholdID: h, ID: id})
		if err != nil {
			return translate(err, "read agreement proposal back")
		}
		out, err = toAgreementProposal(row)
		return err
	})
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	return out, nil
}

// applyAgreementChange is Sign's steps 5 and 6, run only on the signature
// that completes the set. Every stamp carries AND removed_at IS NULL, is
// :execrows, and n == 0 returns ErrAgreementChanged from inside the
// transaction -- step 2 should already have caught it, so this is the loud
// failure if a future edit loses that lock, rather than five of six writes
// committing.
func applyAgreementChange(ctx context.Context, q *sqlcgen.Queries, h, proposalID, sectionID pgtype.UUID,
	kind domain.AgreementProposalKind, p sqlcgen.LockAgreementProposalRow, at time.Time) error {
	add := func() error {
		_, err := q.InsertAgreementFromProposal(ctx, sqlcgen.InsertAgreementFromProposalParams{
			HouseholdID: h,
			SectionID:   sectionID,
			Body:        p.Body,
			ProposalID:  proposalID,
			CreatedAt:   timestamptz(at),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			// The section is not this household's any more, which to the
			// caller is the same thing as the target having moved.
			return domain.ErrAgreementChanged
		}
		return translate(err, "insert agreement from proposal")
	}
	remove := func() error {
		n, err := q.RemoveAgreement(ctx, sqlcgen.RemoveAgreementParams{
			At:          timestamptz(at),
			ProposalID:  proposalID,
			HouseholdID: h,
			ID:          p.TargetAgreementID,
		})
		if err != nil {
			return translate(err, "remove agreement")
		}
		if n == 0 {
			return domain.ErrAgreementChanged
		}
		return nil
	}

	// Fail closed: kind came out of a column, so an unrecognised one refuses
	// rather than committing a signature that changed nothing.
	switch kind {
	case domain.ProposalAdd:
		if err := add(); err != nil {
			return err
		}
	case domain.ProposalRemove:
		if err := remove(); err != nil {
			return err
		}
	case domain.ProposalEdit:
		// Both or neither: the old wording is stamped removed and the new one
		// inserted, inside the transaction that is already holding the row.
		if err := remove(); err != nil {
			return err
		}
		if err := add(); err != nil {
			return err
		}
	default:
		return domain.ErrUnknownAgreementProposalKind
	}

	n, err := q.AcceptAgreementProposal(ctx, sqlcgen.AcceptAgreementProposalParams{
		At: timestamptz(at), HouseholdID: h, ID: proposalID,
	})
	if err != nil {
		return translate(err, "accept agreement proposal")
	}
	if n == 0 {
		// Not a sentinel: step 1's lock makes this an invariant, not a state
		// a caller can reach. SetBillNextDue shipped the other way round.
		return fmt.Errorf("accept agreement proposal: %s matched no open row", uuidToString(proposalID))
	}
	return nil
}
```

- [ ] **Step 6: Write `CreateProposal`, `Park` and `Withdraw`**

Append to `agreement_write_repo.go`:

```go
// CreateProposal writes the proposal row AND the proposer's implicit
// signature (decision 5) in one transaction: either all of it happens or none
// of it does. A proposal without its proposer's signature would ask both
// owners to be the second signer of a set of one, and no route here could
// repair it. Every statement runs on q, never r.q, for the reason Sign's own
// comment gives.
func (r *AgreementRepo) CreateProposal(ctx context.Context, in usecase.AgreementProposalWrite) (usecase.AgreementProposalRecord, error) {
	// Both ids arrive from a request body, so both are checked BEFORE any SQL:
	// uuid() folds "banana" into the same zero UUID an ABSENT value produces
	// (convert.go's uuidLooksValid comment), and the two must not answer the
	// same way. The check is per kind, because an edit and a remove carry no
	// SectionID at all -- the section is the target's, which is why Validate
	// refuses a caller-supplied one -- and uuidLooksValid("") is false, so an
	// unconditional check would refuse every edit and remove here.
	kind, err := domain.ParseAgreementProposalKind(in.Kind)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	if kind == domain.ProposalAdd {
		if !uuidLooksValid(in.SectionID) {
			return usecase.AgreementProposalRecord{}, domain.ErrNotFound
		}
	} else if !uuidLooksValid(in.TargetAgreementID) {
		// Not ErrNotFound: to this household an unparseable target is
		// indistinguishable from one that has been removed, and both mean the
		// same thing to the caller -- what you proposed against is not there.
		return usecase.AgreementProposalRecord{}, domain.ErrAgreementChanged
	}

	var out usecase.AgreementProposalRecord
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		h := uuid(in.HouseholdID)

		// The propose-time half of decision 13, and where an edit or a remove
		// gets its section. The comparison is against in.PreviousBody as
		// stored, never re-trimmed: the service trimmed on the way in, and a
		// second trim would accept wording the proposer never saw.
		sectionID := uuid(in.SectionID)
		if kind != domain.ProposalAdd {
			target, err := q.LockAgreementTarget(ctx, sqlcgen.LockAgreementTargetParams{
				HouseholdID: h, ID: uuid(in.TargetAgreementID), Body: in.PreviousBody,
			})
			if errors.Is(err, pgx.ErrNoRows) {
				return domain.ErrAgreementChanged
			}
			if err != nil {
				return translate(err, "lock agreement target")
			}
			sectionID = target.SectionID
		}

		// A section outside this household matches zero rows in the
		// INSERT ... SELECT, which is indistinguishable from one that does
		// not exist: ErrNotFound for both.
		id, err := q.InsertAgreementProposal(ctx, sqlcgen.InsertAgreementProposalParams{
			HouseholdID:       h,
			Kind:              string(kind),
			SectionID:         sectionID,
			TargetAgreementID: nullableUUID(optionalID(in.TargetAgreementID)),
			Body:              in.Body,
			PreviousBody:      in.PreviousBody,
			Note:              in.Note,
			ProposedBy:        uuid(in.ProposedByMembershipID),
			CreatedAt:         timestamptz(in.CreatedAt),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		if err != nil {
			return translate(err, "insert agreement proposal")
		}

		// The second write, and the one the atomicity test faults: zero rows
		// means the proposer is not an owner of this household, and the
		// proposal written a statement ago goes back with it.
		n, err := q.SignAgreementProposal(ctx, sqlcgen.SignAgreementProposalParams{
			ProposalID:   id,
			MembershipID: uuid(in.ProposedByMembershipID),
			HouseholdID:  h,
			SignedAt:     timestamptz(in.CreatedAt),
		})
		if err != nil {
			return translate(err, "sign agreement proposal")
		}
		if n == 0 {
			return domain.ErrForbidden
		}

		row, err := q.GetAgreementProposal(ctx, sqlcgen.GetAgreementProposalParams{HouseholdID: h, ID: id})
		if err != nil {
			return translate(err, "read agreement proposal back")
		}
		out, err = toAgreementProposal(row)
		return err
	})
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	return out, nil
}

// Park is Discuss: the proposal stays open and appears in the retro page's
// To-discuss block (decision 7); parking twice replaces the note. One guarded
// UPDATE, no transaction, because it writes one row.
//
// at is the port's and is deliberately unused: parking stamps nothing, since
// resolved_at is what "this is settled" means and a parked proposal is not.
func (r *AgreementRepo) Park(ctx context.Context, householdID, proposalID, note string, at time.Time) (usecase.AgreementProposalRecord, error) {
	n, err := r.q.ParkAgreementProposal(ctx, sqlcgen.ParkAgreementProposalParams{
		Note:        note,
		HouseholdID: uuid(householdID),
		ID:          uuid(proposalID),
	})
	if err != nil {
		return usecase.AgreementProposalRecord{}, translate(err, "park agreement proposal")
	}
	if n == 0 {
		return r.diagnoseGuardedUpdate(ctx, householdID, proposalID, "", "park")
	}
	return r.Proposal(ctx, householdID, proposalID)
}

// Withdraw carries the proposer clause in the WHERE, never a service if: a
// check-then-write races. The handler decides and answers first (decision
// 22), so this method never branches on byMembershipID.
func (r *AgreementRepo) Withdraw(ctx context.Context, householdID, proposalID, byMembershipID string, at time.Time) (usecase.AgreementProposalRecord, error) {
	n, err := r.q.WithdrawAgreementProposal(ctx, sqlcgen.WithdrawAgreementProposalParams{
		At:          timestamptz(at),
		HouseholdID: uuid(householdID),
		ID:          uuid(proposalID),
		By:          uuid(byMembershipID),
	})
	if err != nil {
		return usecase.AgreementProposalRecord{}, translate(err, "withdraw agreement proposal")
	}
	if n == 0 {
		return r.diagnoseGuardedUpdate(ctx, householdID, proposalID, byMembershipID, "withdraw")
	}
	return r.Proposal(ctx, householdID, proposalID)
}

// diagnoseGuardedUpdate turns a zero-row guarded UPDATE into the right
// refusal with ONE re-read -- RetroRepo.Update's shape -- and its four legs
// in order: gone, resolved, someone else's while they are still an owner, and
// the re-read's own failure passed through untouched, because "that was
// already settled" is a false claim to make of an unreachable database.
// byMembershipID is "" for park, which has no proposer leg.
func (r *AgreementRepo) diagnoseGuardedUpdate(ctx context.Context, householdID, proposalID, byMembershipID, op string) (usecase.AgreementProposalRecord, error) {
	rec, err := r.Proposal(ctx, householdID, proposalID)
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return usecase.AgreementProposalRecord{}, domain.ErrNotFound
	case err != nil:
		return usecase.AgreementProposalRecord{}, fmt.Errorf("%s: re-read: %w", op, err)
	}
	status, err := domain.ParseAgreementProposalStatus(rec.Status)
	if err != nil {
		return usecase.AgreementProposalRecord{}, err
	}
	if !status.IsOpen() {
		return usecase.AgreementProposalRecord{}, domain.ErrAgreementNotOpen
	}
	if byMembershipID != "" && rec.ProposedByMembershipID != byMembershipID {
		// Open, someone else's, and that someone is still an owner here --
		// or the SQL's second leg would have matched.
		return usecase.AgreementProposalRecord{}, domain.ErrForbidden
	}
	return usecase.AgreementProposalRecord{}, fmt.Errorf("%s: %s matched no row and no leg explains it", op, proposalID)
}
```

Then add one line to `convert.go`'s compile-time block, after `_ usecase.RetroActionRepository = (*RetroActionRepo)(nil)`. `*AgreementRepo` only satisfies the port now that all eight methods exist, and this is the block whose own comment says a drift from `ports.go` must fail here rather than in the wiring:

```go
	_ usecase.AgreementRepository = (*AgreementRepo)(nil)
```

- [ ] **Step 7: Run the tests and watch them pass**

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestCreateProposal|TestSign|TestPark|TestWithdraw' -count=1 -v`

Expected: PASS — **nine** test functions, eleven cases counting `TestSignRefusesAChangedTarget`'s three subtests, which `-v` names individually as `TestSignRefusesAChangedTarget/target_in_another_household` and so on. Each boots its own container, so budget around a minute.

- [ ] **Step 8: Mutation-check the connection and the target lock**

**First — every statement of `Sign` on the transaction's own connection.** In `Sign` step 4, change `q.CountAgreementOwners` to `r.q.CountAgreementOwners`. Rerun.

Expected: a **test** failure. The pool-backed call waits for a connection that the nine held ones and the transaction's own leave nobody to give it, so it blocks until `signCtx`'s five-second deadline — the run takes about five seconds longer than a passing one — and comes back as a `translate`-wrapped `context.DeadlineExceeded`:

```
--- FAIL: TestSignIsOneTransactionOnItsOwnConnection (8.61s)
    agreement_write_repo_test.go:264: err = count agreement owners: context deadline exceeded, want ErrAgreementChanged -- a deadline here means a statement reached back to the pool
```

Nothing else reddens: every other test leaves the pool alone. Restore the line.

**Third — the status read that precedes the signature upsert** (the spec's own
mutation 4, and the reason a three-owner fixture exists at all). In `Sign` step 1,
delete the `if !status.IsOpen()` refusal, leaving the `SELECT … FOR UPDATE` that
takes the lock.

Expected: a **test** failure in exactly one place. Every two-owner test stays
green, because there the only non-proposer signature is also the completing one,
and the apply path refuses a resolved proposal on its own:

```
--- FAIL: TestSignOnAWithdrawnProposalIsRefusedAndWritesNothing (3.28s)
    agreement_write_repo_test.go:4164: signatures = 2, want 1 -- a non-completing
    signature landed on a withdrawn proposal
```

If that test stays green too, the fixture has fewer than three owners and the
mutation is invisible; check the fixture before believing the code. Restore the
refusal.

**Second — the body predicate in the target lock.** In `LockAgreementTarget`, change `AND body = $3` to `AND (body = $3 OR true)`. Not `AND $3::text = $3::text`, and not deleting the clause: with no column reference left, sqlc renames the parameter (`Column3` / `Dollar_3`) and drops `Body` from `LockAgreementTargetParams`, which gives you a build failure instead of the test failure this check is for. `make sqlc`, then rerun.

Expected: a **test** failure in exactly two places, because `CreateProposal` and `Sign` share this one query — the propose-time check and the sign-time check are the same statement, and this mutation neuters both:

```
--- FAIL: TestSignRefusesAChangedTarget/target_reworded (3.11s)
--- FAIL: TestCreateProposalRefusesAStaleTarget (3.04s)
```

**This knowingly overrides the spec's watch-for on this mutation.** The spec says
that if both go red, "decision 13's two claims are checked in one place and the
split into two tests is fictional" — written when the two checks were assumed to
be two statements. They are one: `LockAgreementTarget` is a single query with two
callers, which is decision 13's own "one lock, one check" applied honestly, and
duplicating the SQL to make a mutation discriminate would be writing code for the
test rather than for the household. So the two reds are expected here, and what
still discriminates is the other two legs of the same test: `target_in_another_household`
and `target_removed` **must stay green** under this mutation, because they turn on
`household_id` and `removed_at IS NULL`, which this edit leaves alone. If either of
those reddens, the lock predicate has lost more than the body comparison.

The discriminating claim is the other two legs: `TestSignRefusesAChangedTarget/target_in_another_household` and `/target_removed` must stay **green**, because `household_id = $1` and `removed_at IS NULL` are untouched. If all three legs redden, the three predicates are not being checked separately and the split into three subtests is fictional. Restore and run `make sqlc` again.

- [ ] **Step 9: Commit**

```bash
cd api && go test ./internal/adapter/postgres/ -count=1 && cd .. && make lint
git add api/internal/adapter/postgres/queries/agreements.sql \
        api/internal/adapter/postgres/agreement_write_repo.go \
        api/internal/adapter/postgres/agreement_write_repo_test.go \
        api/internal/adapter/postgres/convert.go \
        api/internal/adapter/postgres/sqlcgen
git commit -m "feat(agreements): propose and sign, each one transaction on one connection

CreateProposal writes the proposal row and the proposer's own signature
together (decision 5). Sign records one Agree and, on the signature that
completes the signing set, applies the change -- every statement on the
transaction's own connection, never reaching back into the pool. Park and
Withdraw are guarded UPDATEs with one diagnosing re-read. AgreementRepo now
satisfies usecase.AgreementRepository, asserted in convert.go.

Mutation checks, both TEST failures rather than build failures:

1. Sign step 4's q.CountAgreementOwners -> r.q.CountAgreementOwners. Red:
   TestSignIsOneTransactionOnItsOwnConnection, about five seconds later than
   a passing run -- 'err = count agreement owners: context deadline exceeded,
   want ErrAgreementChanged'. Nine held connections plus the transaction's
   own leave nothing for a pool-backed call.
2. LockAgreementTarget's AND body = \$3 -> AND (body = \$3 OR true), then
   make sqlc. Red: TestSignRefusesAChangedTarget/target_reworded and
   TestCreateProposalRefusesAStaleTarget, which share that one query.
   target_in_another_household and target_removed stayed green, which is what
   having three separate legs is for."
```

---

### Task 7: The read route, its guard and the DTOs

**Files:**
- Create: `api/internal/adapter/http/agreement_handlers.go`, `api/internal/adapter/http/agreements_api_test.go`
- Modify: `api/internal/adapter/http/router.go` (a `Deps` field after `Visions *usecase.VisionService`, `router.go:59`; one route in the existing marriage group after `m.Get("/marriage/vision", …)`, `router.go:371`), `api/cmd/api/main.go` (the repo beside `visionRepo`, `main.go:114`; the service beside `visionSvc`, `main.go:257`; the `Deps` field beside `Visions`, `main.go:306`), `api/internal/adapter/http/api_test.go` (the same two, `api_test.go:331` and `api_test.go:380` — the test env builds the same `Deps`), `api/internal/adapter/http/auth_api_test.go:258` (the unauthenticated-walk floor)

**Interfaces:**

- Consumes, from Tasks 3–4 and 5–6 — copy verbatim, invent nothing:

```go
type AgreementOwner struct{ MembershipID, Name string }
type AgreementLine struct{ ID, Body string; Number int }
type AgreementSectionView struct {
    ID, Name string
    Count    int
    Visible  bool
    Agreements []AgreementLine
}
type AgreementProposalView struct {
    ID, Kind, Status, SectionID, SectionName, TargetAgreementID string
    Body, PreviousBody, Note, ParkNote                          string
    ProposedByMembershipID, ProposedByName                      string
    ProposedAt                                                  time.Time
    AwaitingNames                                               []string
    SignedByMembershipIDs                                       []string
    TargetChanged                                               bool
}
type AgreementHistoryEntry struct {
    Version                                        int
    ProposalID, Kind, SectionID, SectionName       string
    Body, PreviousBody, Note, ProposedByName       string
    SignedByNames                                  []string
    AcceptedAt                                     time.Time
}
type AgreementsView struct {
    Locked    bool
    Owners    []AgreementOwner
    Version   int
    UpdatedAt *time.Time
    Sections  []AgreementSectionView
    Proposals []AgreementProposalView   // pending and parked only
    History   []AgreementHistoryEntry   // newest first
}

func NewAgreementService(agreements AgreementRepository, members MembershipRepository) *AgreementService
func (s *AgreementService) Get(ctx context.Context, householdID string) (AgreementsView, error)
// and, from Tasks 5-6: postgres.NewAgreementRepo(db), which satisfies usecase.AgreementRepository
```

  The type names are `AgreementOwner`, `AgreementLine` and `AgreementHistoryEntry` — not
  `AgreementOwnerView` / `AgreementItemView` / `AgreementHistoryView`, which do not exist. The one
  field the plan header's condensed interface map does not list is `SignedByMembershipIDs`: Task 3's
  own Produces block carries it on `AgreementProposalView`, commented "canAgree's *did the viewer
  sign*; never on the wire", and `toAgreementProposalDTO` below is its only reader. It is deliberately
  the one view field with no DTO field: what the browser gets is the decided boolean, not the ids it
  would otherwise be tempted to compare itself (`ProposalCard` is forbidden from comparing membership
  ids — spec, `ProposalCard` section).

  A write's `AgreementProposalView` may carry `Status` `"accepted"` or `"withdrawn"`, which `Get`'s
  own walk over `doc.Open` never produces. Tasks 3 and 4 factor that composition into
  `(*AgreementService).proposalView`, which both `Get` and every write call, so the row and the
  document in one response are composed by the same code. Nothing here has to do anything about it —
  it is why `agreementProposalDTO.Status` is documented four-valued while `agreementsDocumentDTO.Proposals`
  is two-valued.

- Produces:
  - `GET /api/v1/marriage/agreements`, answering `agreementsResponse`.
  - Every DTO in Step 3. Task 8 reuses all of them; Tasks 9–16 mirror them field for field in Zod.
  - `toAgreementsDTO`, `toAgreementSectionDTO`, `toAgreementProposalDTO`, `handleGetAgreements`.
  - In the test package: `agreementRoutes()`, `zeroProposalID`, the six decode targets and
    `mustReadAgreements`. **Task 8 extends `agreementRoutes()` from one route to seven** — see the
    comment the function carries, and Task 8 Step 1.

- [ ] **Step 1: Write the failing guard matrix and the locked read**

Create `agreements_api_test.go` in package `httpadapter_test`. `newTestEnv` (`api_test.go:184`),
`env.do` (`api_test.go:483`), `env.authed` (`api_test.go:506`), `env.authedGet` (`api_test.go:531`),
`env.signIn` (`api_test.go:583`), `assertErrorResponse` (`api_test.go:643`), `requestRouteAs`
(`transactions_api_test.go:72`) and `membershipDouble` (`marriage_api_test.go:100`) all exist
already — reuse them, never redefine them.

```go
package httpadapter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andreasoentoro/hearth/api/internal/domain"
)

// zeroProposalID is a well-formed uuid belonging to nobody. Other files in
// this package spell the same value into a local `zeroUUID`; three tests here
// share it, so it is named once at the top instead.
const zeroProposalID = "00000000-0000-0000-0000-000000000000"

// agreementRoutes is the one list every route-shaped test in this file walks,
// so a route added later is walked by all of them without being added twice.
//
// It holds ONE route in this task and grows to seven in Task 8, when the six
// writes are actually routed. Listing an unrouted path here early does not
// merely relax the matrix -- chi answers an unmatched path from r.NotFound
// (router.go:114) BEFORE any group middleware runs, so an unrouted write 404s
// on every leg, including "no session", and the matrix below could not pass at
// this task's commit.
func agreementRoutes() []struct{ method, path string } {
	return []struct{ method, path string }{
		{http.MethodGet, "/api/v1/marriage/agreements"},
	}
}

// The decode targets below mirror agreement_handlers.go's DTOs field for
// field. They are test-side copies rather than the handlers' own unexported
// structs because this package is httpadapter_test, outside the package under
// test -- the shape billResponseBody and transactionsListBody already use in
// this directory.
type agreementLineBody struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Body   string `json:"body"`
}

type agreementSectionBody struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Count      int                 `json:"count"`
	Visible    bool                `json:"visible"`
	Agreements []agreementLineBody `json:"agreements"`
}

type agreementProposalBody struct {
	ID                string   `json:"id"`
	Kind              string   `json:"kind"`
	Status            string   `json:"status"`
	SectionID         string   `json:"sectionId"`
	SectionName       string   `json:"sectionName"`
	TargetAgreementID string   `json:"targetAgreementId"`
	Body              string   `json:"body"`
	PreviousBody      string   `json:"previousBody"`
	Note              string   `json:"note"`
	ParkNote          string   `json:"parkNote"`
	ProposedByName    string   `json:"proposedByName"`
	AwaitingNames     []string `json:"awaitingNames"`
	TargetChanged     bool     `json:"targetChanged"`
	CanAgree          bool     `json:"canAgree"`
	CanWithdraw       bool     `json:"canWithdraw"`
}

type agreementHistoryBody struct {
	Version       int      `json:"version"`
	ProposalID    string   `json:"proposalId"`
	Kind          string   `json:"kind"`
	Body          string   `json:"body"`
	PreviousBody  string   `json:"previousBody"`
	SignedByNames []string `json:"signedByNames"`
}

type agreementOwnerBody struct {
	MembershipID string `json:"membershipId"`
	Name         string `json:"name"`
}

// UpdatedAt is *string, not *time.Time: this test only ever asks whether it is
// null, and the raw-bytes test below is what pins the literal null itself.
type agreementsDocumentBody struct {
	Locked    bool                    `json:"locked"`
	Owners    []agreementOwnerBody    `json:"owners"`
	Version   int                     `json:"version"`
	UpdatedAt *string                 `json:"updatedAt"`
	Sections  []agreementSectionBody  `json:"sections"`
	Proposals []agreementProposalBody `json:"proposals"`
	History   []agreementHistoryBody  `json:"history"`
}

type agreementsReadBody struct {
	Agreements agreementsDocumentBody `json:"agreements"`
}

// mustReadAgreements is setup, not an assertion: a read that did not answer
// 200 with a parseable body fails here rather than as a confusing empty
// struct in whatever asserts on the document next. Decoding it is also what
// proves the 2xx carries JSON at all -- apiFetch throws on an ok response it
// cannot parse.
func mustReadAgreements(t *testing.T, env *testEnv, session *http.Cookie) agreementsDocumentBody {
	t.Helper()
	rec := env.authedGet(t, "/api/v1/marriage/agreements", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET agreements: status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	var out agreementsReadBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode agreements: %v (body = %s)", err, rec.Body.String())
	}
	return out.Agreements
}

// TestAgreementRoutesRequireMarriageAndOwner is
// TestMarriageRoutesRequireMarriageAndOwner's shape (marriage_api_test.go)
// applied to this feature: every agreements route against no session, a
// limited member, and an owner.
//
// The owner leg asserts only that NO GUARD refused. On newTestEnv's one-owner
// household the read answers 200 and, once Task 8 routes them, the writes
// answer 409, 404 or 400 -- each write's real status is its own test in Task
// 8, and pinning it twice would make this matrix fail whenever a body shape
// changed.
func TestAgreementRoutesRequireMarriageAndOwner(t *testing.T) {
	env := newTestEnv(t)
	for _, route := range agreementRoutes() {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			if rec := env.do(route.method, route.path, nil); rec.Code != http.StatusUnauthorized {
				t.Fatalf("no session = %d, want 401 (body = %s)", rec.Code, rec.Body.String())
			}
			session, csrf := env.signIn(t, env.limitedEmail, env.limitedPassword)
			if rec := requestRouteAs(t, env, route.method, route.path, session, csrf); rec.Code != http.StatusForbidden {
				t.Fatalf("limited member = %d, want 403 (body = %s)", rec.Code, rec.Body.String())
			}
			session, csrf = env.signIn(t, env.ownerEmail, env.ownerPassword)
			rec := requestRouteAs(t, env, route.method, route.path, session, csrf)
			if rec.Code == http.StatusUnauthorized || rec.Code == http.StatusForbidden {
				t.Fatalf("owner = %d, want any non-guard status (body = %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// requireCapability and requireOwner both answer 403 FORBIDDEN, so the matrix
// above cannot say which one refused the limited member. Only a caller HOLDING
// marriage without being an owner can see requireOwner, and three independent
// layers refuse to build that membership (domain.NewMembership, MemberService,
// and 00002_identity.sql's limited_members_have_no_marriage CHECK). So it is
// doctored through the same membershipDouble seam
// TestMarriageRouteRejectsALimitedMemberHoldingMarriage uses
// (marriage_api_test.go:155), swapped in for this one request only.
//
// HouseholdID on the doctored membership must match env.householdID:
// requireSession cross-checks it against the session row and answers 401 on a
// mismatch, which would look like the guard was never reached rather than like
// it refused.
func TestAgreementsRouteRejectsALimitedMemberHoldingMarriage(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.limitedEmail, env.limitedPassword)

	router := env.routerWithMemberships(membershipDouble{
		MembershipRepository: env.deps.Memberships,
		membership: domain.Membership{
			HouseholdID:  env.householdID,
			UserID:       "irrelevant-scope-userid-comes-from-the-real-session",
			Role:         domain.RoleLimited,
			Capabilities: domain.Capabilities{domain.CapMarriage},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/marriage/agreements", nil)
	req.AddCookie(session)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertErrorResponse(t, rec, http.StatusForbidden, "FORBIDDEN")
}

// TestLockedAgreementsReadCarriesLiteralEmptyArrays reads the RAW WIRE BYTES,
// because Go decodes null and [] into the same nil slice -- only the bytes
// prove the frontend's Zod schemas get []. Vision's
// TestGetVisionForANeverSetYearCarriesLiteralEmptyArrays
// (vision_api_test.go:118) is this test's shape.
//
// newTestEnv's household has one owner, so this is decision 2's state: locked,
// nothing written yet. 200 rather than 403 because what it lacks is a second
// owner, not permission, and the empty state IS the page (decision 3 -- the
// read is never gated on locked).
func TestLockedAgreementsReadCarriesLiteralEmptyArrays(t *testing.T) {
	env := newTestEnv(t)
	session, _ := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authedGet(t, "/api/v1/marriage/agreements", session)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body = %s)", rec.Code, rec.Body.String())
	}
	raw := rec.Body.String()
	for _, want := range []string{
		`"locked":true`, `"version":1`, `"updatedAt":null`,
		`"sections":[]`, `"proposals":[]`, `"history":[]`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("wire body does not literally carry %s -- got %s", want, raw)
		}
	}

	// The owners list travels on every response and its LENGTH is the owner
	// count -- no second count travels beside it, so a screen that says
	// "one owner" is reading these rows.
	doc := mustReadAgreements(t, env, session)
	if len(doc.Owners) != 1 || doc.Owners[0].Name != "Andreas" {
		t.Fatalf("owners = %+v, want exactly the single owner Andreas", doc.Owners)
	}
}
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd api && go test ./internal/adapter/http/ -run 'TestAgreement|TestLockedAgreements' -count=1 -v
```

Expected: FAIL, and a **test** failure, not a build failure — the file above references only helpers
that already exist, so it compiles. Three failures, and the first line of each is worth checking
against this list, because a different message means a different defect:

- `TestAgreementRoutesRequireMarriageAndOwner/GET_/api/v1/marriage/agreements`: `no session = 404,
  want 401`. **The 404, not a 401 or a 403** — chi serves an unmatched path from `r.NotFound`
  (`router.go:114`) before `requireSession` ever runs, so the very first leg is what reddens.
- `TestAgreementsRouteRejectsALimitedMemberHoldingMarriage`: `status = 404, want 403`.
- `TestLockedAgreementsReadCarriesLiteralEmptyArrays`: `status = 404, want 200`.

- [ ] **Step 3: Write the DTOs**

Create `agreement_handlers.go`. Package clause `package httpadapter`, then exactly four imports —
`net/http`, `slices`, `time` and `github.com/andreasoentoro/hearth/api/internal/usecase`. Not `chi`
and not `domain`: nothing in this task uses either, and an unused import is a build failure. Task 8
adds both when its handlers need them.

These structs are copied from the spec's API section. Their JSON tags are what Tasks 9–16 mirror in
Zod, so a renamed tag is a silently broken frontend, not a compile error.

```go
// maxAgreementRequestBodyBytes replaces the ordinary maxRequestBodyBytes for
// two routes only: POST /marriage/agreements/proposals and
// POST /marriage/agreements/proposals/{id}/park. Between them they carry four
// rune-capped free-text fields (body, previousBody, note, park note) at 500
// runes each, and a 500-rune CJK field alone is 1500 bytes -- the 1 KiB
// default would answer 413 to a body the domain considers legal. 8 KiB clears
// all four comfortably while still refusing anything absurd, the same
// reasoning maxRetroRequestBodyBytes and maxVisionRequestBodyBytes give for
// their own overrides.
const maxAgreementRequestBodyBytes = 8 * 1024

type createAgreementSectionRequest struct {
	Name string `json:"name"`
}

// SectionID is an add's alone. The handler blanks it for any other kind
// rather than refusing: the propose modal still holds one from add mode, and
// on an edit or a remove the server takes the section from the target anyway.
// PreviousBody is required rather than optional on those two kinds -- an
// agreement body is never empty, so an omitted one would always read as stale.
type proposeAgreementChangeRequest struct {
	Kind              string `json:"kind"`              // "add" | "edit" | "remove", parsed here (decision 21)
	SectionID         string `json:"sectionId"`         // add: required. edit/remove: blanked by the handler
	TargetAgreementID string `json:"targetAgreementId"` // edit/remove: required
	Body              string `json:"body"`              // add/edit: required
	PreviousBody      string `json:"previousBody"`      // edit/remove: required
	Note              string `json:"note"`              // always optional
}

// Note may be "", but the body is always sent: an absent one is 400
// INVALID_BODY. Discuss is a bare button, so an empty park note is ordinary.
type parkAgreementProposalRequest struct {
	Note string `json:"note"`
}

// Number is the design's "01" as an integer, derived at render (decision 11);
// the zero padding is the browser's. No addedAt and no signer ids on the wire:
// the design renders neither, and a field nothing reads is a field nothing
// keeps honest.
type agreementDTO struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	Body   string `json:"body"`
}

// Every section travels, empty ones included (decision 8). Count is its live
// agreements and Visible is Count > 0, both stamped by the service: the page
// renders the visible ones and the propose picker offers them all, off ONE
// array. Two arrays would ship every section twice on every write response,
// for one boolean.
type agreementSectionDTO struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Count      int            `json:"count"`
	Visible    bool           `json:"visible"`
	Agreements []agreementDTO `json:"agreements"`
}

type agreementOwnerDTO struct {
	MembershipID string `json:"membershipId"`
	Name         string `json:"name"`
}

type agreementProposalDTO struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // "add" | "edit" | "remove"
	// "pending" | "parked" in the document; a WRITE response may also carry
	// "accepted" or "withdrawn", which is why the frontend's enum is
	// four-valued and the card's own switch is what narrows it.
	Status                 string    `json:"status"`
	SectionID              string    `json:"sectionId"` // for edit and remove it is the target's section
	SectionName            string    `json:"sectionName"`
	TargetAgreementID      string    `json:"targetAgreementId"` // "" for add
	Body                   string    `json:"body"`              // "" for remove
	PreviousBody           string    `json:"previousBody"`      // "" for add
	Note                   string    `json:"note"`
	ParkNote               string    `json:"parkNote"` // may be "" even when parked: Discuss is a bare button
	ProposedByMembershipID string    `json:"proposedByMembershipId"`
	ProposedByName         string    `json:"proposedByName"` // "" if the membership no longer resolves
	ProposedAt             time.Time `json:"proposedAt"`
	AwaitingNames          []string  `json:"awaitingNames"` // owners yet to sign, evaluated live (decision 4)
	TargetChanged          bool      `json:"targetChanged"` // previousBody no longer matches the live target
	CanAgree               bool      `json:"canAgree"`
	CanWithdraw            bool      `json:"canWithdraw"`
}

// Version is the one this change PRODUCED; SignedByNames is read from the
// signatures it collected, never from today's owners; and there is no display
// number, because a change accepted two years ago has no position in today's
// document.
type agreementHistoryEntryDTO struct {
	Version        int       `json:"version"`
	ProposalID     string    `json:"proposalId"`
	Kind           string    `json:"kind"`
	SectionID      string    `json:"sectionId"`
	SectionName    string    `json:"sectionName"`
	Body           string    `json:"body"`
	PreviousBody   string    `json:"previousBody"`
	Note           string    `json:"note"`
	ProposedByName string    `json:"proposedByName"`
	SignedByNames  []string  `json:"signedByNames"`
	AcceptedAt     time.Time `json:"acceptedAt"`
}

type agreementsDocumentDTO struct {
	Locked bool `json:"locked"`
	// Its length IS the owner count; no second count travels beside these rows,
	// so the locked screen and the pending card cannot disagree about it.
	Owners    []agreementOwnerDTO        `json:"owners"`
	Version   int                        `json:"version"`   // count(accepted) + 1 (decision 10)
	UpdatedAt *time.Time                 `json:"updatedAt"` // null until the first accepted change
	Sections  []agreementSectionDTO      `json:"sections"`  // every slice on the wire is [], never null
	Proposals []agreementProposalDTO     `json:"proposals"` // pending and parked only
	History   []agreementHistoryEntryDTO `json:"history"`   // every accepted change, newest first
}

type agreementsResponse struct {
	Agreements agreementsDocumentDTO `json:"agreements"`
}

type agreementSectionWriteResponse struct {
	Section    agreementSectionDTO   `json:"section"`
	Agreements agreementsDocumentDTO `json:"agreements"`
}

// Proposal is the row the write touched at the status it now holds, and the
// only place "accepted" or "withdrawn" reaches the wire. No screen renders it
// -- every card re-renders off Agreements -- and it exists so the HTTP tests
// can assert the transition field by field, which a whole-document assertion
// would bury.
type agreementProposalWriteResponse struct {
	Proposal   agreementProposalDTO  `json:"proposal"`
	Agreements agreementsDocumentDTO `json:"agreements"`
}
```

- [ ] **Step 4: Write the mappers and the read handler**

Append to the same file. Every slice is built with `make(..., 0, n)` and appended to, never left as a
nil range variable: a nil slice serialises as `null`, and the frontend's Zod schemas parse `[]`.

```go
// toAgreementProposalDTO stamps canAgree and canWithdraw HERE, not in the
// service: the service composes what is true of the household, and only the
// HTTP layer knows who is asking (CLAUDE.md -- no service takes an actor
// parameter to decide whether a caller may act).
//
// canAgree's second clause is decision 16: because the signer set is evaluated
// live, a proposal can become fully signed by nobody's action -- three owners,
// one proposes, one agrees, the third leaves -- and completion is only ever
// decided during a signing, so without this clause that proposal would sit
// pending, needing nobody, forever. The idempotent re-Agree is what closes it.
//
// canWithdraw tests the proposer against the LIVE owner set (decision 15),
// never a column: once the proposer is no longer an owner, any owner may
// withdraw it, or a proposal left behind by a departed partner could never be
// removed by anyone.
//
// Both say the caller MAY act, not that the write will succeed -- freshness is
// targetChanged's job -- and both are false on a locked household, so nothing
// clickable would 409 (decision 3).
func toAgreementProposalDTO(p usecase.AgreementProposalView, doc usecase.AgreementsView, viewer string) agreementProposalDTO {
	awaiting := make([]string, 0, len(p.AwaitingNames))
	awaiting = append(awaiting, p.AwaitingNames...)
	proposerIsOwner := slices.ContainsFunc(doc.Owners, func(o usecase.AgreementOwner) bool {
		return o.MembershipID == p.ProposedByMembershipID
	})
	return agreementProposalDTO{
		ID:                     p.ID,
		Kind:                   p.Kind,
		Status:                 p.Status,
		SectionID:              p.SectionID,
		SectionName:            p.SectionName,
		TargetAgreementID:      p.TargetAgreementID,
		Body:                   p.Body,
		PreviousBody:           p.PreviousBody,
		Note:                   p.Note,
		ParkNote:               p.ParkNote,
		ProposedByMembershipID: p.ProposedByMembershipID,
		ProposedByName:         p.ProposedByName,
		ProposedAt:             p.ProposedAt,
		AwaitingNames:          awaiting,
		TargetChanged:          p.TargetChanged,
		CanAgree:               !doc.Locked && (!slices.Contains(p.SignedByMembershipIDs, viewer) || len(awaiting) == 0),
		CanWithdraw:            !doc.Locked && (p.ProposedByMembershipID == viewer || !proposerIsOwner),
	}
}

func toAgreementSectionDTO(s usecase.AgreementSectionView) agreementSectionDTO {
	items := make([]agreementDTO, 0, len(s.Agreements))
	for _, a := range s.Agreements {
		items = append(items, agreementDTO{ID: a.ID, Number: a.Number, Body: a.Body})
	}
	return agreementSectionDTO{
		ID: s.ID, Name: s.Name, Count: s.Count, Visible: s.Visible, Agreements: items,
	}
}

func toAgreementsDTO(doc usecase.AgreementsView, viewer string) agreementsDocumentDTO {
	owners := make([]agreementOwnerDTO, 0, len(doc.Owners))
	for _, o := range doc.Owners {
		owners = append(owners, agreementOwnerDTO{MembershipID: o.MembershipID, Name: o.Name})
	}
	sections := make([]agreementSectionDTO, 0, len(doc.Sections))
	for _, s := range doc.Sections {
		sections = append(sections, toAgreementSectionDTO(s))
	}
	proposals := make([]agreementProposalDTO, 0, len(doc.Proposals))
	for _, p := range doc.Proposals {
		proposals = append(proposals, toAgreementProposalDTO(p, doc, viewer))
	}
	history := make([]agreementHistoryEntryDTO, 0, len(doc.History))
	for _, h := range doc.History {
		names := make([]string, 0, len(h.SignedByNames))
		names = append(names, h.SignedByNames...)
		history = append(history, agreementHistoryEntryDTO{
			Version:        h.Version,
			ProposalID:     h.ProposalID,
			Kind:           h.Kind,
			SectionID:      h.SectionID,
			SectionName:    h.SectionName,
			Body:           h.Body,
			PreviousBody:   h.PreviousBody,
			Note:           h.Note,
			ProposedByName: h.ProposedByName,
			SignedByNames:  names,
			AcceptedAt:     h.AcceptedAt,
		})
	}
	return agreementsDocumentDTO{
		Locked:    doc.Locked,
		Owners:    owners,
		Version:   doc.Version,
		UpdatedAt: doc.UpdatedAt,
		Sections:  sections,
		Proposals: proposals,
		History:   history,
	}
}

// handleGetAgreements answers 200 for a locked household too (decision 3):
// what it lacks is a second owner, not permission, and the empty state IS the
// page. The read is never gated on locked -- a household that dropped to one
// owner keeps seeing what it agreed to and its frozen proposals, and the
// banner explaining why is the frontend's job off doc.locked.
func handleGetAgreements(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		doc, err := deps.Agreements.Get(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, agreementsResponse{
			Agreements: toAgreementsDTO(doc, scope.Membership.ID),
		})
	}
}
```

- [ ] **Step 5: Wire the service and the route**

The route joins the **existing** marriage group. A second group is a second place to forget a guard,
which is the reason the group's own comment (`router.go:357-364`) already gives for stacking
`requireOwner` on top of `requireCapability(CapMarriage)`.

In `router.go`, in `Deps`, straight after `Visions *usecase.VisionService` (line 59):

```go
	Agreements *usecase.AgreementService
```

In `router.go`, inside the marriage group, after `m.Get("/marriage/vision", handleGetVision(deps))`
(line 371) — outside the `requireCSRF` sub-group, since CSRF exempts GET:

```go
				m.Get("/marriage/agreements", handleGetAgreements(deps))
```

In `cmd/api/main.go`, beside `visionRepo` (line 114), beside `visionSvc` (line 257), and in `Deps`
beside `Visions` (line 306):

```go
	agreementRepo := postgres.NewAgreementRepo(db)

	agreementSvc := usecase.NewAgreementService(agreementRepo, memberships)

			Agreements:     agreementSvc,
```

In `internal/adapter/http/api_test.go`, beside `visionSvc` (line 331) and in `deps` (line 380) — the
test env must build the same `Deps` the binary does, or every route test runs against a router the
production one does not match:

```go
	agreementSvc := usecase.NewAgreementService(postgres.NewAgreementRepo(db), memberships)

		Agreements:     agreementSvc,
```

- [ ] **Step 6: Run the tests and watch them pass**

```bash
cd api && go test ./internal/adapter/http/ -run 'TestAgreement|TestLockedAgreements' -count=1 -v
```

Expected: PASS — one subtest in the matrix (the GET; Task 8 grows the list to seven), the doctored
membership test, and the locked read. Then the whole package, because `Deps` gained a field and
`api_test.go` changed:

```bash
cd api && go test ./internal/adapter/http/ -count=1 -timeout=5m
```

Expected: PASS, the whole package — **including** `TestEveryProtectedRouteRejectsAnUnauthenticatedCaller`.
Its floor is a minimum (`if checked < 62`), so a new route raises `checked` and cannot trip it. That
is precisely the vacuous pass the floor's own comment warns about, and why Step 7 raises it rather
than treating the green as proof the route was enumerated.

- [ ] **Step 7: Re-measure the unauthenticated walk floor**

```bash
cd api && go test ./internal/adapter/http/ -run TestEveryProtectedRouteRejectsAnUnauthenticatedCaller -count=1 -v
```

It logs `checked N protected routes`. Set **both** numbers at `auth_api_test.go:258-260` — the
`if checked < 62` and the `want at least 62` in the message beneath it — to exactly the `N` it just
printed. Read it off the log; never compute it by adding to the previous number. That floor's own
comment records it drifting from a stale 18 to a real 59 precisely because people bumped it by
arithmetic instead of re-running the walk.

- [ ] **Step 8: Mutation-check the guard and the empty collections**

Two mutations, one per thing this task actually decides. Restore after each.

**(a) The guard.** Delete `m.Use(requireOwner)` (`router.go:367`). Expected red:
`TestAgreementsRouteRejectsALimitedMemberHoldingMarriage` **and** the existing
`TestMarriageRouteRejectsALimitedMemberHoldingMarriage`, both on `status = 200, want 403` — the
doctored membership holds `CapMarriage`, so with `requireOwner` gone nothing is left to refuse it and
the handler runs. A **test** failure, not a build failure. **Nothing in `TestAgreementRoutesRequireMarriageAndOwner`
may go red**: its limited member holds calendar and chores only, so `requireCapability` still refuses
them. If the matrix does redden, it was passing for the wrong reason and the doctored test is not
what is proving the guard.

**(b) The empty collections.** In `toAgreementsDTO`, replace
`sections := make([]agreementSectionDTO, 0, len(doc.Sections))` with
`var sections []agreementSectionDTO`. Expected red:
`TestLockedAgreementsReadCarriesLiteralEmptyArrays`, on
`wire body does not literally carry "sections":[] -- got {"agreements":{…"sections":null…}}`. Again a
**test** failure, not a build failure — the variable is still declared and still used, which is the
point: this is the mutation a compiler cannot catch and a decoding test cannot see either, since Go
decodes `null` and `[]` into the same nil slice.

- [ ] **Step 9: Commit**

```bash
cd /Volumes/Oink_Machine/Intelij/HouseholdDashboard
git add api/internal/adapter/http/agreement_handlers.go \
  api/internal/adapter/http/agreements_api_test.go \
  api/internal/adapter/http/router.go api/internal/adapter/http/api_test.go \
  api/internal/adapter/http/auth_api_test.go api/cmd/api/main.go
git commit -m "feat(agreements): the read route, its DTOs, and a guard proved by a doctored membership" -m "GET /marriage/agreements joins the existing requireCapability(marriage) +
requireOwner group rather than a second one, and answers 200 even when the
household is locked: what it lacks is a second owner, not permission
(decision 3).

Mutations checked, both restored:
- Deleted m.Use(requireOwner) (router.go:367). Red:
  TestAgreementsRouteRejectsALimitedMemberHoldingMarriage and the existing
  TestMarriageRouteRejectsALimitedMemberHoldingMarriage, 'status = 200, want
  403'. The guard matrix stayed green, as it must. A test failure, not a
  build failure.
- Replaced make([]agreementSectionDTO, 0, n) with var in toAgreementsDTO.
  Red: TestLockedAgreementsReadCarriesLiteralEmptyArrays, 'wire body does not
  literally carry \"sections\":[]'. A test failure, not a build failure.

auth_api_test.go's unauthenticated-walk floor was re-measured from the walk's
own t.Logf output, not bumped by arithmetic."
```

---

### Task 8: The six write routes and the error mapping

**Files:**
- Modify: `api/internal/adapter/http/agreement_handlers.go` (six handlers and one shared responder), `api/internal/adapter/http/agreements_api_test.go` (`agreementRoutes()` grows to seven; the write tests), `api/internal/adapter/http/errors.go` (a third bullet on `decodeJSONBodyLimit`'s caller list at lines 46-62, and twelve new cases inserted above line 437's `ErrUnknownContributionSource` comment), `api/internal/adapter/http/router.go:389` (the existing `requireCSRF` sub-group), `api/internal/adapter/http/auth_api_test.go:258,357` (two walk floors), `api/internal/adapter/http/household_api_test.go:197` (the third)

**Interfaces:**

- Consumes Task 7's DTOs, mappers and test decode targets; `usecase.MembershipRepository.List`
  (`ports.go:125`, returning `[]usecase.MemberView` whose `.Membership` is the `domain.Membership`);
  `domain.ParseAgreementProposalKind`, `domain.ProposalAdd`, `domain.RequiredSigners`,
  `domain.AgreementProposal` (Task 2); and from Tasks 3–4, **verbatim**:

```go
func (s *AgreementService) Proposal(ctx context.Context, householdID, proposalID string) (AgreementProposalRecord, error)
func (s *AgreementService) CreateSection(ctx context.Context, householdID, name string, at time.Time) (AgreementSectionView, AgreementsView, error)
func (s *AgreementService) SeedStarterSections(ctx context.Context, householdID string, at time.Time) (AgreementsView, error)
func (s *AgreementService) Propose(ctx context.Context, householdID, proposedByMembershipID string, p domain.AgreementProposal, at time.Time) (AgreementProposalView, AgreementsView, error)
func (s *AgreementService) Sign(ctx context.Context, householdID, proposalID, membershipID string, at time.Time) (AgreementProposalView, AgreementsView, error)
func (s *AgreementService) Park(ctx context.Context, householdID, proposalID, note string, at time.Time) (AgreementProposalView, AgreementsView, error)
func (s *AgreementService) Withdraw(ctx context.Context, householdID, proposalID, byMembershipID string, at time.Time) (AgreementProposalView, AgreementsView, error)
```

  Three things about that list are load-bearing and none of them is negotiable here.
  **Every write returns three values** — the row it touched, the whole recomposed document, and the
  error — because each write moves the version, the `01..N` numbering, the history list and which
  proposals are open (spec, API section). `SeedStarterSections` returns two, having no single row to
  name. **`Propose` takes five arguments, not three**: the household and the proposer are passed
  positionally because the service stamps them onto the `domain.AgreementProposal` before validating
  it, so a body carrying either is ignored rather than trusted. Every method takes `at time.Time` —
  nothing in the service reads a clock.

- Produces: the six write routes, `respondProposal`, and the codes `AGREEMENTS_NEED_TWO_OWNERS`,
  `AGREEMENT_CHANGED`, `AGREEMENT_PROPOSAL_RESOLVED`, `AGREEMENT_SECTION_NAME_TAKEN`,
  `AGREEMENT_SECTION_NAME_REQUIRED`, `AGREEMENT_SECTION_NAME_TOO_LONG`,
  `AGREEMENT_PROPOSAL_SHAPE_INVALID`, `AGREEMENT_EDIT_UNCHANGED`, `AGREEMENT_BODY_REQUIRED`,
  `AGREEMENT_BODY_TOO_LONG`, `AGREEMENT_NOTE_TOO_LONG`, `AGREEMENT_PARK_NOTE_TOO_LONG` and, from the
  handler's own parser rather than `MapDomainError`, `AGREEMENT_KIND_INVALID`.

- [ ] **Step 1: Grow the route list, then write the failing write tests**

First replace `agreementRoutes()`'s body in `agreements_api_test.go` with the full seven. The
function's comment about why it held one route stays true as history; rewrite it to the version
below.

```go
// agreementRoutes is the one list every route-shaped test in this file walks,
// so a route added later is walked by all of them without being added twice.
// The GET is first and stays first: TestAgreementWriteRoutesRequireCSRF drops
// it with [1:], requireCSRF exempting reads entirely.
func agreementRoutes() []struct{ method, path string } {
	p := proposalPath(zeroProposalID)
	return []struct{ method, path string }{
		{http.MethodGet, "/api/v1/marriage/agreements"},
		{http.MethodPost, "/api/v1/marriage/agreements/sections"},
		{http.MethodPost, "/api/v1/marriage/agreements/starter-set"},
		{http.MethodPost, agreementProposalsPath},
		{http.MethodPost, p + "/agree"},
		{http.MethodPost, p + "/park"},
		{http.MethodPost, p + "/withdraw"},
	}
}
```

Then append the rest to the same file, adding `context` and
`github.com/andreasoentoro/hearth/api/internal/adapter/crypto` to its imports. `encoding/json`,
`net/http`, `net/http/httptest`, `strings`, `testing` and `domain` are already there from Task 7.

```go
const agreementProposalsPath = "/api/v1/marriage/agreements/proposals"

func proposalPath(id string) string { return agreementProposalsPath + "/" + id }

// addSecondOwner seeds the partner through the repositories -- api_test.go's
// own owner-seeding shape (api_test.go:420-433) -- and signs them in for real
// cookies, because every test below needs two sessions rather than two
// membership rows.
//
// It is per-file and deliberately NOT part of newTestEnv: other files assert
// on that household having exactly one owner, and moving this into the
// constructor would break them in a way that looks unrelated to this feature.
//
// The cheap argon2 parameters are the ones newTestEnv itself uses -- this is
// still the real hasher, only its cost is turned down, so sign-in exercises
// Verify exactly as it does in production.
func addSecondOwner(t *testing.T, env *testEnv) (session, csrf *http.Cookie) {
	t.Helper()
	hash, err := crypto.NewArgon2Hasher(1, 8*1024, 1).Hash("hunter2hunter2")
	if err != nil {
		t.Fatalf("hash second owner password: %v", err)
	}
	user, err := env.users.Create(context.Background(), "christine@hearth.family", hash, "Christine")
	if err != nil {
		t.Fatalf("create second owner user: %v", err)
	}
	if _, err := env.deps.Memberships.Create(context.Background(), domain.Membership{
		HouseholdID:  env.householdID,
		UserID:       user.ID,
		Role:         domain.RoleOwner,
		Capabilities: domain.AllCapabilities(),
	}); err != nil {
		t.Fatalf("create second owner membership: %v", err)
	}
	return env.signIn(t, "christine@hearth.family", "hunter2hunter2")
}

type agreementSectionWriteBody struct {
	Section    agreementSectionBody   `json:"section"`
	Agreements agreementsDocumentBody `json:"agreements"`
}

type agreementProposalWriteBody struct {
	Proposal   agreementProposalBody  `json:"proposal"`
	Agreements agreementsDocumentBody `json:"agreements"`
}

// mustCreateAgreementSection and mustProposalWrite are setup, not assertions:
// a write that did not land fails here rather than as a confusing failure in
// whatever reads its id next. Decoding every response is also what proves each
// 2xx carries parseable JSON -- apiFetch throws on an ok response it cannot
// parse, so an unparseable 200 is a broken screen, not a passing test.
func mustCreateAgreementSection(t *testing.T, env *testEnv, name string, session, csrf *http.Cookie) agreementSectionBody {
	t.Helper()
	rec := env.authed(t, http.MethodPost, "/api/v1/marriage/agreements/sections",
		map[string]any{"name": name}, session, csrf)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create section %q: status = %d, want 201 (body = %s)", name, rec.Code, rec.Body.String())
	}
	var out agreementSectionWriteBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode section write: %v (body = %s)", err, rec.Body.String())
	}
	return out.Section
}

func mustProposalWrite(t *testing.T, env *testEnv, path string, body any,
	session, csrf *http.Cookie, want int) agreementProposalWriteBody {
	t.Helper()
	rec := env.authed(t, http.MethodPost, path, body, session, csrf)
	if rec.Code != want {
		t.Fatalf("POST %s: status = %d, want %d (body = %s)", path, rec.Code, want, rec.Body.String())
	}
	var out agreementProposalWriteBody
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v (body = %s)", path, err, rec.Body.String())
	}
	return out
}

// findAgreement locates one live agreement by its wording, across sections,
// because the numbering runs continuously across them and no test should
// depend on which section index it landed in.
func findAgreement(t *testing.T, doc agreementsDocumentBody, body string) agreementLineBody {
	t.Helper()
	for _, s := range doc.Sections {
		for _, a := range s.Agreements {
			if a.Body == body {
				return a
			}
		}
	}
	t.Fatalf("no live agreement reads %q -- sections = %+v", body, doc.Sections)
	return agreementLineBody{}
}

func findProposal(doc agreementsDocumentBody, id string) (agreementProposalBody, bool) {
	for _, p := range doc.Proposals {
		if p.ID == id {
			return p, true
		}
	}
	return agreementProposalBody{}, false
}

// TestAgreementWriteRoutesRequireCSRF sends all six writes twice -- no header,
// then a wrong one -- and asserts the CODE, not the status. requireOwner sits
// ahead of requireCSRF in the same group and answers the identical 403, so a
// bare status check stays green with the requireCSRF line deleted, which is
// the one thing this test exists to catch.
func TestAgreementWriteRoutesRequireCSRF(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)
	for _, route := range agreementRoutes()[1:] { // [1:] drops the GET: requireCSRF exempts reads
		for _, token := range []string{"", "definitely-the-wrong-value"} {
			t.Run(route.method+" "+route.path+" token="+token, func(t *testing.T) {
				req := httptest.NewRequest(route.method, route.path, nil)
				req.AddCookie(session)
				req.AddCookie(csrf)
				if token != "" {
					req.Header.Set("X-CSRF-Token", token)
				}
				rec := httptest.NewRecorder()
				env.router.ServeHTTP(rec, req)
				assertErrorResponse(t, rec, http.StatusForbidden, "CSRF_INVALID")
			})
		}
	}
}

// TestAgreementWritesOnATwoOwnerHousehold walks one proposal's whole life on
// the wire. Withdraw is exercised in BOTH directions, because a comparison
// with its sides swapped passes every one-sided refusal test.
func TestAgreementWritesOnATwoOwnerHousehold(t *testing.T) {
	env := newTestEnv(t)
	a, aCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	b, bCSRF := addSecondOwner(t, env)

	// A section is a label, not a promise: creating one is immediate and
	// unsigned, and it stays invisible until an agreed agreement sits in it
	// (decision 8).
	section := mustCreateAgreementSection(t, env, "Money", a, aCSRF)
	if section.Count != 0 || section.Visible {
		t.Fatalf("new section = %+v, want count 0 and visible false", section)
	}
	propose := map[string]any{
		"kind": "add", "sectionId": section.ID,
		"body": "We talk about anything over $200.", "note": "",
	}

	first := mustProposalWrite(t, env, agreementProposalsPath, propose, a, aCSRF, http.StatusCreated)
	if first.Proposal.Status != "pending" {
		t.Fatalf("status = %q, want pending", first.Proposal.Status)
	}
	// The proposer signed implicitly, in the same transaction (decision 5), so
	// only the other owner is awaited -- "needs Christine", never "needs both".
	if len(first.Proposal.AwaitingNames) != 1 || first.Proposal.AwaitingNames[0] != "Christine" {
		t.Fatalf("awaitingNames = %v, want [Christine]", first.Proposal.AwaitingNames)
	}

	// 404 before 403 (decision 22): an unknown id is not somebody else's
	// proposal, and the handler has to read the row before it can know whose
	// it is.
	rec := env.authed(t, http.MethodPost, proposalPath(zeroProposalID)+"/withdraw", nil, a, aCSRF)
	assertErrorResponse(t, rec, http.StatusNotFound, "NOT_FOUND")
	// B did not propose it and A is still an owner: 403, ahead of any 409.
	rec = env.authed(t, http.MethodPost, proposalPath(first.Proposal.ID)+"/withdraw", nil, b, bCSRF)
	assertErrorResponse(t, rec, http.StatusForbidden, "AGREEMENT_NOT_PROPOSER")
	// And the proposer's own withdraw goes through -- the other direction.
	withdrawn := mustProposalWrite(t, env, proposalPath(first.Proposal.ID)+"/withdraw", nil, a, aCSRF, http.StatusOK)
	if withdrawn.Proposal.Status != "withdrawn" {
		t.Fatalf("status = %q, want withdrawn", withdrawn.Proposal.Status)
	}
	if len(withdrawn.Agreements.Proposals) != 0 {
		t.Fatalf("proposals = %+v, want none open after the withdrawal", withdrawn.Agreements.Proposals)
	}

	second := mustProposalWrite(t, env, agreementProposalsPath, propose, a, aCSRF, http.StatusCreated)
	// Discuss parks it: still open, still answerable (decision 7).
	parked := mustProposalWrite(t, env, proposalPath(second.Proposal.ID)+"/park",
		map[string]any{"note": "next retro"}, b, bCSRF, http.StatusOK)
	if parked.Proposal.Status != "parked" || parked.Proposal.ParkNote != "next retro" {
		t.Fatalf("park = %+v, want parked carrying the note", parked.Proposal)
	}

	agreed := mustProposalWrite(t, env, proposalPath(second.Proposal.ID)+"/agree", nil, b, bCSRF, http.StatusOK)
	if agreed.Proposal.Status != "accepted" {
		t.Fatalf("status = %q, want accepted -- the second owner completes the set", agreed.Proposal.Status)
	}
	// The version, the history and the numbering all move together, off the
	// one document the write returned.
	if agreed.Agreements.Version != 2 || len(agreed.Agreements.History) != 1 {
		t.Fatalf("version = %d, history = %d, want 2 and 1",
			agreed.Agreements.Version, len(agreed.Agreements.History))
	}
	landed := findAgreement(t, agreed.Agreements, "We talk about anything over $200.")
	if landed.Number != 1 {
		t.Fatalf("number = %d, want 1 -- the first agreement in the document", landed.Number)
	}

	// Agreeing again is refused, not a no-op success: accepted means the
	// household already got what this click is asking for, so "reload, this
	// was settled" is the honest answer.
	rec = env.authed(t, http.MethodPost, proposalPath(second.Proposal.ID)+"/agree", nil, b, bCSRF)
	assertErrorResponse(t, rec, http.StatusConflict, "AGREEMENT_PROPOSAL_RESOLVED")
}

// TestStarterSetSeedsFourSectionsAndIsIdempotent covers the one write that
// answers the bare document envelope rather than a row plus a document, and
// the one that may create nothing -- which is why it is 200 and not 201, and
// why a second click is a no-op rather than a 409 (decision 17).
func TestStarterSetSeedsFourSectionsAndIsIdempotent(t *testing.T) {
	env := newTestEnv(t)
	a, aCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	addSecondOwner(t, env) // the writes are locked below two owners

	for attempt := 1; attempt <= 2; attempt++ {
		rec := env.authed(t, http.MethodPost, "/api/v1/marriage/agreements/starter-set", nil, a, aCSRF)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d: status = %d, want 200 (body = %s)", attempt, rec.Code, rec.Body.String())
		}
		var out agreementsReadBody
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("attempt %d: decode starter-set: %v (body = %s)", attempt, err, rec.Body.String())
		}
		if len(out.Agreements.Sections) != 4 {
			t.Fatalf("attempt %d: %d sections, want 4 -- %+v",
				attempt, len(out.Agreements.Sections), out.Agreements.Sections)
		}
		// Sections only, never agreements (decision 17): "everything on this
		// page is here because you both agreed" has no bulk-signed exception.
		for _, s := range out.Agreements.Sections {
			if s.Count != 0 || s.Visible {
				t.Fatalf("attempt %d: section %q has count %d and visible %v, want 0 and false",
					attempt, s.Name, s.Count, s.Visible)
			}
		}
		if out.Agreements.Version != 1 {
			t.Fatalf("attempt %d: version = %d, want 1 -- a section is not an accepted change",
				attempt, out.Agreements.Version)
		}
	}
}

// TestAStaleAgreeIsRefusedAndTheProposalSurvives is the spec's "stale agree
// through two real sessions". It cannot be built from one session: two edits
// have to exist against the same wording before either lands, which is the
// race the whole feature exists to refuse (decision 13).
func TestAStaleAgreeIsRefusedAndTheProposalSurvives(t *testing.T) {
	env := newTestEnv(t)
	a, aCSRF := env.signIn(t, env.ownerEmail, env.ownerPassword)
	b, bCSRF := addSecondOwner(t, env)

	const original = "We talk about anything over $200."
	section := mustCreateAgreementSection(t, env, "Money", a, aCSRF)
	added := mustProposalWrite(t, env, agreementProposalsPath, map[string]any{
		"kind": "add", "sectionId": section.ID, "body": original, "note": "",
	}, a, aCSRF, http.StatusCreated)
	landed := mustProposalWrite(t, env, proposalPath(added.Proposal.ID)+"/agree", nil, b, bCSRF, http.StatusOK)
	target := findAgreement(t, landed.Agreements, original)

	// Two edits against the SAME wording, both proposed before either is
	// agreed, so both pass their propose-time target check.
	edit := func(body string) agreementProposalWriteBody {
		return mustProposalWrite(t, env, agreementProposalsPath, map[string]any{
			"kind": "edit", "targetAgreementId": target.ID,
			"previousBody": original, "body": body, "note": "",
		}, a, aCSRF, http.StatusCreated)
	}
	firstEdit := edit("We talk about anything over $300.")
	secondEdit := edit("We talk about anything over $500.")

	// B agrees the first. An accepted edit stamps the target removed and
	// inserts the new wording, so the second edit's previous_body now names
	// something that is gone.
	appliedFirst := mustProposalWrite(t, env, proposalPath(firstEdit.Proposal.ID)+"/agree", nil, b, bCSRF, http.StatusOK)
	if appliedFirst.Agreements.Version != 3 || len(appliedFirst.Agreements.History) != 2 {
		t.Fatalf("version = %d, history = %d, want 3 and 2",
			appliedFirst.Agreements.Version, len(appliedFirst.Agreements.History))
	}

	// 409 AGREEMENT_CHANGED, not 404: the proposal WAS found, only its target
	// moved. Merging the two changes would produce a document neither owner
	// agreed to.
	rec := env.authed(t, http.MethodPost, proposalPath(secondEdit.Proposal.ID)+"/agree", nil, b, bCSRF)
	assertErrorResponse(t, rec, http.StatusConflict, "AGREEMENT_CHANGED")

	// Nothing was written and nothing was hidden: the refused proposal is
	// still listed, still open, and targetChanged now says so before the next
	// click (decision 14) -- the read-side echo of the check that just fired.
	doc := mustReadAgreements(t, env, b)
	stale, ok := findProposal(doc, secondEdit.Proposal.ID)
	if !ok {
		t.Fatalf("the refused proposal is no longer listed -- proposals = %+v", doc.Proposals)
	}
	if stale.Status != "pending" || !stale.TargetChanged {
		t.Fatalf("stale proposal = %+v, want status pending with targetChanged true", stale)
	}
	if doc.Version != 3 {
		t.Fatalf("version = %d, want 3 -- the refused agree must not have moved the document", doc.Version)
	}
}

// TestProposeRefusesAnUnknownKindAndAnOversizedBody covers both refusals the
// handler answers without the service: the kind, parsed at the boundary
// (decision 21), and decodeJSONBodyLimit's 8 KiB ceiling. Neither reaches
// requireTwoOwners, which is why a one-owner env is the right fixture -- a 409
// here would mean the order of the two checks had silently swapped.
func TestProposeRefusesAnUnknownKindAndAnOversizedBody(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	rec := env.authed(t, http.MethodPost, agreementProposalsPath,
		map[string]any{"kind": "delete", "body": "x"}, session, csrf)
	assertErrorResponse(t, rec, http.StatusUnprocessableEntity, "AGREEMENT_KIND_INVALID")

	rec = env.authed(t, http.MethodPost, agreementProposalsPath,
		map[string]any{"kind": "add", "body": strings.Repeat("x", 9*1024)}, session, csrf)
	assertErrorResponse(t, rec, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE")
}
```

- [ ] **Step 2: Run them and watch them fail**

```bash
cd api && go test ./internal/adapter/http/ \
  -run 'TestAgreement|TestStarterSet|TestAStaleAgree|TestPropose|TestLockedAgreements' -count=1 -v
```

Expected: FAIL, and a **test** failure, not a build failure — everything the file references now
exists. What reddens, and why each message is the one to expect:

- `TestAgreementRoutesRequireMarriageAndOwner`: the `GET` subtest still PASSES (Task 7 routed it);
  the **six new write subtests** fail on `no session = 404, want 401`, because chi answers an
  unmatched path from `r.NotFound` before any group middleware runs. Not `403`, and not the owner
  leg — the first leg is the one that fires.
- `TestAgreementWriteRoutesRequireCSRF`: `status = 404, want 403` on the first subtest.
- `TestAgreementWritesOnATwoOwnerHousehold` and `TestAStaleAgreeIsRefusedAndTheProposalSurvives`:
  `create section "Money": status = 404, want 201`, from `mustCreateAgreementSection`.
- `TestStarterSetSeedsFourSectionsAndIsIdempotent`: `attempt 1: status = 404, want 200`.
- `TestProposeRefusesAnUnknownKindAndAnOversizedBody`: `status = 404, want 422`.
- `TestLockedAgreementsReadCarriesLiteralEmptyArrays` stays green throughout — its route landed in
  Task 7.

- [ ] **Step 3: Write the two handlers that decide something**

Append to `agreement_handlers.go`, adding `github.com/go-chi/chi/v5` and
`github.com/andreasoentoro/hearth/api/internal/domain` to its imports.

```go
// respondProposal answers the body every proposal write shares: the row the
// write touched at the status it now holds, plus the whole freshly composed
// document, because each write moves the version, the 01..N numbering, the
// history list and which proposals are open.
//
// The document is composed AFTER the write and outside its transaction, so a
// concurrent agree may already have overtaken it. That is accepted rather than
// worked around: the frontend's refetch stays the authority, and a response
// that tried to be authoritative would need the read inside the write's
// transaction for no gain the screen can see.
func respondProposal(w http.ResponseWriter, viewer string, status int,
	p usecase.AgreementProposalView, doc usecase.AgreementsView) {
	WriteJSON(w, status, agreementProposalWriteResponse{
		Proposal:   toAgreementProposalDTO(p, doc, viewer),
		Agreements: toAgreementsDTO(doc, viewer),
	})
}

// handleProposeAgreementChange parses the kind itself and answers 422 from
// here (decision 21), the way parseVisionYear answers a bad year. A kind
// arrives from two places -- a request body, where a bad value is the caller's
// mistake, and a database column, where a bad value is a corrupt row -- and one
// sentinel serving both jobs would make a broken row indistinguishable from a
// typo. So domain.ErrUnknownAgreementProposalKind deliberately has no
// MapDomainError case: anything reaching the mapper with it came from a column.
//
// SectionID is blanked for anything but an add, because the modal still holds
// one from add mode and on an edit or a remove the server copies the section
// from the target anyway. Validate's refusal of a caller-supplied section on
// those two kinds stays the fail-closed backstop behind that.
func handleProposeAgreementChange(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req proposeAgreementChangeRequest
		if !decodeJSONBodyLimit(w, r, &req, maxAgreementRequestBodyBytes) {
			return
		}
		kind, err := domain.ParseAgreementProposalKind(req.Kind)
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "AGREEMENT_KIND_INVALID",
				"That is not a change we can propose.", nil)
			return
		}
		sectionID := req.SectionID
		if kind != domain.ProposalAdd {
			sectionID = ""
		}
		// HouseholdID and ProposedByMembershipID are left zero in the struct on
		// purpose: the service stamps both from the two arguments below -- the
		// route and the session -- so a body carrying either is ignored rather
		// than trusted. Filling them in here from req would be the mistake.
		p, doc, err := deps.Agreements.Propose(r.Context(), scope.HouseholdID, scope.Membership.ID,
			domain.AgreementProposal{
				Kind:              string(kind),
				SectionID:         sectionID,
				TargetAgreementID: req.TargetAgreementID,
				Body:              req.Body,
				PreviousBody:      req.PreviousBody,
				Note:              req.Note,
			}, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		respondProposal(w, scope.Membership.ID, http.StatusCreated, p, doc)
	}
}

// handleWithdrawAgreementProposal owns the refusal order 404 -> 403 -> 409
// (decision 22). It must read the proposal before it can know whose it is, so
// the order is a property of THIS handler and not of the guards, and without
// it "every write refuses 409 when the household is locked" is false on the
// wire.
//
// The proposer is compared against the LIVE owner set, never a column: once
// they are no longer an owner, any owner may withdraw it (decision 15), or a
// proposal left behind by a departed partner could never be removed by anyone
// -- the permanently-stuck state this codebase already shipped once, in
// invites. That is a "who is asking" question, which is why it lives here and
// no service takes an actor parameter for it. AgreementRepository.Withdraw's
// own SQL clause is the backstop behind this, and it never branches on $by.
func handleWithdrawAgreementProposal(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		proposalID := chi.URLParam(r, "id")

		record, err := deps.Agreements.Proposal(r.Context(), scope.HouseholdID, proposalID)
		if err != nil {
			MapDomainError(w, r, err) // an unknown or foreign id is ErrNotFound -> 404
			return
		}
		members, err := deps.Memberships.List(r.Context(), scope.HouseholdID)
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		all := make([]domain.Membership, 0, len(members))
		for _, m := range members {
			all = append(all, m.Membership)
		}
		if record.ProposedByMembershipID != scope.Membership.ID &&
			slices.Contains(domain.RequiredSigners(all), record.ProposedByMembershipID) {
			WriteError(w, http.StatusForbidden, "AGREEMENT_NOT_PROPOSER",
				"Only the owner who proposed this can withdraw it.", nil)
			return
		}

		p, doc, err := deps.Agreements.Withdraw(r.Context(), scope.HouseholdID,
			proposalID, scope.Membership.ID, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err) // a locked household refuses here, 409, and only here
			return
		}
		respondProposal(w, scope.Membership.ID, http.StatusOK, p, doc)
	}
}
```

- [ ] **Step 4: Write the four handlers that only pass work through**

Also `agreement_handlers.go`. Agree and withdraw read no body at all: a client-echoed `previousBody`
would be the one copy nobody verified.

```go
// handleCreateAgreementSection answers 201, because a section creates a row --
// and creating one is immediate and unsigned, since a heading is not a promise
// (decision 8). decodeJSONBody's 1 KiB default is right here: the body is one
// name, capped at 60 runes.
func handleCreateAgreementSection(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req createAgreementSectionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		section, doc, err := deps.Agreements.CreateSection(r.Context(), scope.HouseholdID,
			req.Name, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusCreated, agreementSectionWriteResponse{
			Section:    toAgreementSectionDTO(section),
			Agreements: toAgreementsDTO(doc, scope.Membership.ID),
		})
	}
}

// handleSeedStarterAgreementSections answers 200, not 201: the starter set is
// idempotent (decision 17) and a second click may create nothing. It answers
// the bare document rather than a row plus a document because nothing renders
// from the four rows' order -- render order is always the document's.
func handleSeedStarterAgreementSections(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		doc, err := deps.Agreements.SeedStarterSections(r.Context(), scope.HouseholdID, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		WriteJSON(w, http.StatusOK, agreementsResponse{
			Agreements: toAgreementsDTO(doc, scope.Membership.ID),
		})
	}
}

// handleAgreeAgreementProposal records one Agree. A repeat Agree is an
// idempotent 200 that may complete the set (decision 16) -- the signature write
// is an upsert -- while an Agree on a proposal already accepted or withdrawn is
// 409 AGREEMENT_PROPOSAL_RESOLVED from the service. Every count and comparison
// that decides the outcome lives inside the repository's transaction; nothing
// is decided here.
func handleAgreeAgreementProposal(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		p, doc, err := deps.Agreements.Sign(r.Context(), scope.HouseholdID,
			chi.URLParam(r, "id"), scope.Membership.ID, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		respondProposal(w, scope.Membership.ID, http.StatusOK, p, doc)
	}
}

// handleParkAgreementProposal is Discuss: the proposal stays open, stays
// answerable, and is rendered in the read-only To-discuss block on the Retros
// page (decision 7). Nothing here touches a retro table and there is no
// foreign key to a retro row -- the next retro usually does not exist yet,
// which is exactly when a couple parks something.
func handleParkAgreementProposal(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, _ := RequestScope(r)
		var req parkAgreementProposalRequest
		if !decodeJSONBodyLimit(w, r, &req, maxAgreementRequestBodyBytes) {
			return
		}
		p, doc, err := deps.Agreements.Park(r.Context(), scope.HouseholdID,
			chi.URLParam(r, "id"), req.Note, deps.Clock.Now())
		if err != nil {
			MapDomainError(w, r, err)
			return
		}
		respondProposal(w, scope.Membership.ID, http.StatusOK, p, doc)
	}
}
```

- [ ] **Step 5: Map the twelve refusals**

In `errors.go`, insert this block **above** the `// domain.ErrUnknownContributionSource has no case
here` comment at line 437. That comment belongs to the `ErrAlreadyExists` backstop it precedes, so do
not split the two. `errors.Is` matches in source order, and anything placed below the backstop would
answer the generic `ALREADY_EXISTS` instead of naming what collided.

```go
	// --- Agreements ---------------------------------------------------------
	// domain.ErrUnknownAgreementProposalKind and
	// domain.ErrUnknownAgreementProposalStatus are deliberately absent from
	// this block. A bad kind in a request body is answered by
	// handleProposeAgreementChange's own ParseAgreementProposalKind call, with
	// 422 AGREEMENT_KIND_INVALID, exactly as parseVisionYear answers a bad year
	// (decision 21). So either sentinel reaching this function came from a
	// database column -- a row no migration allows and no writer here wrote --
	// and the logged 500 below is the right answer to an impossible row, not a
	// 4xx telling a household their request was wrong when it was not.
	case errors.Is(err, domain.ErrAgreementsNeedTwoOwners):
		WriteError(w, http.StatusConflict, "AGREEMENTS_NEED_TWO_OWNERS",
			"Agreements need at least two owners.", nil)
	case errors.Is(err, domain.ErrAgreementChanged):
		WriteError(w, http.StatusConflict, "AGREEMENT_CHANGED",
			"The agreement this was written against has changed, so nothing was signed.", nil)
	case errors.Is(err, domain.ErrAgreementNotOpen):
		WriteError(w, http.StatusConflict, "AGREEMENT_PROPOSAL_RESOLVED",
			"This change was already settled. Reload to see it.", nil)
	case errors.Is(err, domain.ErrAgreementSectionNameTaken):
		WriteError(w, http.StatusConflict, "AGREEMENT_SECTION_NAME_TAKEN",
			"You already have a section with that name.", nil)
	case errors.Is(err, domain.ErrAgreementSectionNameRequired):
		WriteError(w, http.StatusUnprocessableEntity, "AGREEMENT_SECTION_NAME_REQUIRED",
			"Give this section a name.", nil)
	case errors.Is(err, domain.ErrAgreementSectionNameTooLong):
		WriteError(w, http.StatusUnprocessableEntity, "AGREEMENT_SECTION_NAME_TOO_LONG",
			"That section name is too long.", nil)
	case errors.Is(err, domain.ErrAgreementProposalShapeInvalid):
		WriteError(w, http.StatusUnprocessableEntity, "AGREEMENT_PROPOSAL_SHAPE_INVALID",
			"That is not a change we can propose.", nil)
	case errors.Is(err, domain.ErrAgreementEditUnchanged):
		WriteError(w, http.StatusUnprocessableEntity, "AGREEMENT_EDIT_UNCHANGED",
			"This wording is the same as the agreement it changes.", nil)
	case errors.Is(err, domain.ErrAgreementBodyRequired):
		WriteError(w, http.StatusUnprocessableEntity, "AGREEMENT_BODY_REQUIRED",
			"Write the agreement before proposing it.", nil)
	case errors.Is(err, domain.ErrAgreementBodyTooLong):
		WriteError(w, http.StatusUnprocessableEntity, "AGREEMENT_BODY_TOO_LONG",
			"That agreement is too long.", nil)
	// The two note caps get two codes rather than one shared "note too long",
	// because they are different fields on different screens: the proposal's
	// note is in the Propose modal, the park note in the card's Discuss
	// expander, and a 422 that cannot say which field is a 422 the screen
	// cannot place.
	case errors.Is(err, domain.ErrAgreementNoteTooLong):
		WriteError(w, http.StatusUnprocessableEntity, "AGREEMENT_NOTE_TOO_LONG",
			"That note is too long.", nil)
	case errors.Is(err, domain.ErrAgreementParkNoteTooLong):
		WriteError(w, http.StatusUnprocessableEntity, "AGREEMENT_PARK_NOTE_TOO_LONG",
			"That note is too long.", nil)
```

Then add a third bullet to `decodeJSONBodyLimit`'s caller list (`errors.go:46-62`), after the
`PATCH /retros/{month}` one. That doc comment is how the next reader finds every override, so a
caller missing from it is an override nobody knows exists:

```go
//   - POST /marriage/agreements/proposals and
//     POST /marriage/agreements/proposals/{id}/park carry rune-capped free
//     text (body, previousBody, note, park note) at 500 runes each, and a
//     500-rune CJK field alone exceeds the 1 KiB default.
//     agreement_handlers.go's maxAgreementRequestBodyBytes is the caller that
//     needs this.
```

- [ ] **Step 6: Route the six writes**

Into the **existing** `requireCSRF` sub-group of the marriage group, after
`w.Put("/marriage/vision/{year}", handleSaveVision(deps))` (`router.go:389`):

```go
					// Each action is its own POST, not a patchable status, or
					// saving a note could withdraw a proposal. Nothing is ever
					// deleted (decision 9), so there is no DELETE, no restore
					// route -- Restore is a propose with kind "add" (decision
					// 18) -- and no 204 anywhere in this feature.
					w.Post("/marriage/agreements/sections", handleCreateAgreementSection(deps))
					w.Post("/marriage/agreements/starter-set", handleSeedStarterAgreementSections(deps))
					w.Post("/marriage/agreements/proposals", handleProposeAgreementChange(deps))
					w.Post("/marriage/agreements/proposals/{id}/agree", handleAgreeAgreementProposal(deps))
					w.Post("/marriage/agreements/proposals/{id}/park", handleParkAgreementProposal(deps))
					w.Post("/marriage/agreements/proposals/{id}/withdraw", handleWithdrawAgreementProposal(deps))
```

- [ ] **Step 7: Run the tests and watch them pass**

```bash
cd api && go test ./internal/adapter/http/ -count=1 -timeout=5m
```

Expected: PASS for every agreements test — the seven-route matrix, the twelve CSRF subtests
(six routes × two token shapes), the two-owner walk, the starter set, the stale agree, the two
handler-level refusals and the locked read.

The three router-wide walks pass too, and that is not the same as being correct: every floor is a
`<` minimum (`checked < 62`, `checked < 44`, `checked < 10`), so six new routes raise each count and
none of them can trip. A green walk here proves only that the walk still runs — the exact vacuous
pass those floors' own comments name — which is why Step 8 raises all three to what they now log.

- [ ] **Step 8: Re-measure all three router-wide walk floors**

```bash
cd api && go test ./internal/adapter/http/ -count=1 -v \
  -run 'TestEveryProtectedRouteRejectsAnUnauthenticatedCaller|TestEveryMutatingRouteRequiresCSRF|TestOwnerOnlyRoutesRejectALimitedMember'
```

Read each `t.Logf` line and set that floor to **exactly the number it just printed**. Not a number
that looks like enough, and **not** arithmetic on Task 7's figure — re-measuring is the whole rule,
and the 62 floor's own comment records it drifting from a stale 18 to a real 59 because people added
to the previous number instead of re-running the walk.

| Logged line | Floor to set | Both places |
|---|---|---|
| `checked N protected routes` | `auth_api_test.go:258` | the `if checked < …` and the `want at least …` in its `t.Fatalf` message |
| `checked N mutating routes` | `auth_api_test.go:357` | same two |
| `checked N owner-gated candidate routes, M admin routes` | `household_api_test.go:197` (`checked`, not `adminChecked`) | same two |

`TestOwnerOnlyRoutesRejectALimitedMember` is the one most easily missed. Its limited member holds
money and not marriage, so the six agreements writes satisfy it at `requireCapability` exactly as
retro and vision already do — which is why Task 7's doctored-membership test stays the only proof
`requireOwner` is wired at all.

- [ ] **Step 9: Mutation-check the proposer comparison, the resolved mapping and the stale mapping**

Three mutations. Restore after each, and run the named test alone to see the message.

**(a) Swap the sides of the withdraw comparison.** In `handleWithdrawAgreementProposal`, change
`record.ProposedByMembershipID != scope.Membership.ID` to `== scope.Membership.ID`. Expected red:
`TestAgreementWritesOnATwoOwnerHousehold`, on **B's** leg — the first of the two withdraw directions
to run — with `error code = "FORBIDDEN", want "AGREEMENT_NOT_PROPOSER"`.

Trace that before trusting it, because the message is not the obvious one: B is not the proposer, so
the swapped condition is false, the handler skips its own 403 and calls `Withdraw` anyway; the
repository's `AND (proposed_by_membership_id = $by OR …)` clause then matches zero rows and its
diagnose leg returns `domain.ErrForbidden`, which `MapDomainError` already maps to the generic
403 `FORBIDDEN` (`errors.go:133`) — so the status matches and only the code differs. That difference
*is* the finding: the 403 and its nameable code are this handler's to answer, and the SQL clause
behind it is a backstop, not the rule. A's leg never runs, the test having already failed. A **test**
failure, not a build failure.

**(b) Map `domain.ErrAgreementNotOpen` to `http.StatusInternalServerError`** in the block from Step 5.
Expected red: the same test, on its last assertion, `status = 500, want 409`. A test failure.

**(c) Map `domain.ErrAgreementChanged` to `http.StatusNotFound` with code `NOT_FOUND`.** Expected red:
`TestAStaleAgreeIsRefusedAndTheProposalSurvives`, on `status = 404, want 409`. This is the one the
spec argues for explicitly — a 404 would be false, since the proposal *was* found and only its target
moved — so a mapping that conflates the two must be visible. A test failure.

- [ ] **Step 10: Commit**

```bash
cd /Volumes/Oink_Machine/Intelij/HouseholdDashboard
git add api/internal/adapter/http/
git commit -m "feat(agreements): six write routes, the 409s the screen can explain, and 404 -> 403 -> 409 on withdraw" -m "The six writes join the marriage group's existing requireCSRF sub-group.
Every one returns the row it touched plus the whole recomposed document,
because each moves the version, the numbering, the history and which
proposals are open. handleProposeAgreementChange parses the kind itself and
answers 422 AGREEMENT_KIND_INVALID, so a corrupt column and a caller's typo
never share a sentinel (decision 21); handleWithdrawAgreementProposal owns
the 404 -> 403 -> 409 order, because it must read the proposal before it can
know whose it is (decision 22).

Mutations checked, all three restored:
- Swapped the sides of the withdraw proposer comparison. Red:
  TestAgreementWritesOnATwoOwnerHousehold on B's leg, 'error code =
  \"FORBIDDEN\", want \"AGREEMENT_NOT_PROPOSER\"' -- the repository's clause
  refusing instead of this handler, which is the finding. A test failure, not
  a build failure.
- Mapped ErrAgreementNotOpen to 500. Red: the same test on the repeat-agree
  assertion, 'status = 500, want 409'. A test failure.
- Mapped ErrAgreementChanged to 404 NOT_FOUND. Red:
  TestAStaleAgreeIsRefusedAndTheProposalSurvives, 'status = 404, want 409'. A
  test failure.

All three router-wide walk floors were re-measured from their own t.Logf
output -- the unauthenticated walk, the CSRF walk, and
TestOwnerOnlyRoutesRejectALimitedMember -- never bumped by arithmetic on the
previous number."
```

---

### Task 9: Frontend schemas, keys, copy and the hook

**Files:** Create, all in `web/src/features/marriage/`: `agreementQueryKeys.ts`, `agreementSchemas.ts`, `agreementCopy.ts`, `useAgreements.ts`, `useAgreements.test.ts`

**Interfaces:**

Consumes the JSON of Tasks 7–8 — `agreementsResponse` (`{ "agreements": {…} }`), `agreementSectionWriteResponse` (`{ "section": {…}, "agreements": {…} }`) and `agreementProposalWriteResponse` (`{ "proposal": {…}, "agreements": {…} }`), each wrapping the DTOs in `agreement_handlers.go`.

Produces:

```ts
// agreementQueryKeys.ts
export function agreementsQueryKey(): readonly ["agreements"]

// agreementSchemas.ts -- values (the schemas) and the types inferred from them
export const agreementKindSchema, agreementStatusSchema, agreementSchema,
             agreementSectionSchema, agreementOwnerSchema, agreementProposalSchema,
             agreementHistoryEntrySchema, agreementsDocumentSchema,
             agreementsResponseSchema, agreementSectionWriteResponseSchema,
             agreementProposalWriteResponseSchema
export type AgreementKind = "add" | "edit" | "remove"
export type Agreement, AgreementSection, AgreementOwner, AgreementProposal,
            AgreementHistoryEntry, AgreementsDocument

// agreementCopy.ts
export const AGREEMENT_COPY
export function joinNames(names: string[]): string        // "Andreas and Christine"
export function agreementDateLabel(iso: string): string   // "28 Jun"
export function historyDateLabel(iso: string): string     // "28 Jun 2026"

// useAgreements.ts
export type ProposeBody = {
  kind: AgreementKind; sectionId: string; targetAgreementId: string;
  body: string; previousBody: string; note: string
}
export function handleWriteError(err: unknown, reload: () => Promise<void>, fallback: string): string
export function useAgreements(): {
  data: AgreementsDocument | undefined
  isLoading: boolean
  error: unknown
  reload: () => Promise<void>
  createSection: (name: string) => Promise<{ section: AgreementSection; agreements: AgreementsDocument }>
  seedStarterSet: () => Promise<AgreementsDocument>
  propose: (body: ProposeBody) => Promise<AgreementsDocument>
  agree: (proposalId: string) => Promise<AgreementsDocument>
  park: (proposalId: string, note: string) => Promise<AgreementsDocument>
  withdraw: (proposalId: string) => Promise<AgreementsDocument>
  isProposing: boolean
  isCreatingSection: boolean
}
```

Three of those are worth reading twice, because every later frontend task is written against them.

`isLoading`, not `loading`. The Marriage hooks that came before (`useVision.ts`, `useRetros.ts`) return `loading`; this one keeps TanStack's own name because six mutations sit beside it and `isProposing`/`isCreatingSection` would otherwise read as a different family of fields from the query flag next to them.

`createSection` is the one write that resolves to more than the document, because the New-section modal's **"Create & add first agreement"** needs the new section's id to seed the Propose modal (Task 14). Every other write resolves to the document alone: both screens re-render off it, and **no screen renders a write response's `proposal` row** — the spec says so outright, and the row exists on the wire only so the Go HTTP tests can assert the transition field by field.

`handleWriteError`'s second parameter is `() => Promise<void>`, which is exactly what `useAgreements().reload` is. It calls that reload — so it is behaviour, and lives in `useAgreements.ts`, not in `agreementCopy.ts`.

Read `useVision.ts` first: the module-level `fetchX` helper, the `useQuery` + `useMutation` shape, and `onSuccess` **returning** the invalidation (TanStack awaits whatever `onSuccess` returns before the mutation settles, so a caller's `await` lands on a refreshed cache) all come from there.

- [ ] **Step 1: Write the failing tests** — `web/src/features/marriage/useAgreements.test.ts`, in full:

```ts
import { createElement } from "react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "../../api/client";
import { stubFetchRoutes } from "../../test/fetchStub";
import { AGREEMENT_COPY } from "./agreementCopy";
import type { AgreementProposal, AgreementsDocument } from "./agreementSchemas";
import { handleWriteError, useAgreements } from "./useAgreements";

// These four are lifted into web/src/features/marriage/agreementFixtures.tsx
// by Task 10, which needs the same shapes for the page tests and for the
// Retros block in Task 16; this file's copies are deleted in that same task.
// Keep the names identical so that edit is an import, not a rewrite.
const DOC_URL = "/api/v1/marriage/agreements";
const OWNERS = [
  { membershipId: "m-1", name: "Andreas" },
  { membershipId: "m-2", name: "Christine" },
];

function documentFixture(o: Partial<AgreementsDocument> = {}): AgreementsDocument {
  return {
    locked: false,
    owners: OWNERS,
    version: 1,
    updatedAt: null,
    sections: [],
    proposals: [],
    history: [],
    ...o,
  };
}

function proposalFixture(o: Partial<AgreementProposal> = {}): AgreementProposal {
  return {
    id: "p-1",
    kind: "add",
    status: "pending",
    sectionId: "s-1",
    sectionName: "Money",
    targetAgreementId: "",
    body: "One shared account for bills",
    previousBody: "",
    note: "",
    parkNote: "",
    proposedByMembershipId: "m-1",
    proposedByName: "Andreas",
    proposedAt: "2026-09-05T10:00:00+08:00",
    awaitingNames: ["Christine"],
    targetChanged: false,
    canAgree: true,
    canWithdraw: false,
    ...o,
  };
}

function renderUseAgreements() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderHook(() => useAgreements(), {
    wrapper: ({ children }) => createElement(QueryClientProvider, { client: queryClient }, children),
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("useAgreements", () => {
  // Two claims, one click. The status enum is FOUR-valued: an Agree that
  // completes the signing set answers with the row at "accepted", and apiFetch
  // throws on an ok response it cannot parse -- so a two-valued enum would fail
  // the very click that finished the agreement. And there is ONE key: the
  // Retros To-discuss block reads the same query, so counting the GETs is what
  // proves the write invalidated that key rather than updating its own caller
  // in place.
  it("parses the accepted proposal an Agree answers with, and invalidates the one key both screens read", async () => {
    const stub = stubFetchRoutes({
      [`GET ${DOC_URL}`]: [
        { status: 200, body: { agreements: documentFixture({ proposals: [proposalFixture()] }) } },
        { status: 200, body: { agreements: documentFixture({ version: 2 }) } },
      ],
      [`POST ${DOC_URL}/proposals/p-1/agree`]: {
        status: 200,
        body: {
          proposal: proposalFixture({ status: "accepted", awaitingNames: [], canAgree: false }),
          agreements: documentFixture({ version: 2 }),
        },
      },
    });

    const { result } = renderUseAgreements();
    await waitFor(() => expect(result.current.data?.version).toBe(1));

    let after: AgreementsDocument | undefined;
    await act(async () => {
      after = await result.current.agree("p-1");
    });

    // The write resolves to the DOCUMENT, not the row it touched.
    expect(after?.version).toBe(2);
    await waitFor(() => expect(result.current.data?.version).toBe(2));
    expect(stub.mock.calls.filter(([input]) => String(input) === DOC_URL)).toHaveLength(2);
  });

  // The one write that resolves to more than the document: Task 14's
  // "Create & add first agreement" closes this modal and opens Propose seeded
  // with the new section's id, which only this response carries. The body is
  // asserted with toEqual against the WHOLE body -- toMatchObject would pass a
  // request that silently sent extra fields the server would then refuse.
  it("createSection resolves to the new section AND the document, and posts only the name", async () => {
    let postBody: unknown;
    const created = { id: "s-9", name: "In-laws", count: 0, visible: false, agreements: [] };
    stubFetchRoutes({
      [`GET ${DOC_URL}`]: [
        { status: 200, body: { agreements: documentFixture() } },
        { status: 200, body: { agreements: documentFixture({ sections: [created] }) } },
      ],
      [`POST ${DOC_URL}/sections`]: {
        status: 201,
        body: { section: created, agreements: documentFixture({ sections: [created] }) },
        capture: (body) => {
          postBody = body;
        },
      },
    });

    const { result } = renderUseAgreements();
    await waitFor(() => expect(result.current.data).toBeDefined());

    let out: { section: { id: string }; agreements: AgreementsDocument } | undefined;
    await act(async () => {
      out = await result.current.createSection("In-laws");
    });

    expect(postBody).toEqual({ name: "In-laws" });
    expect(out?.section.id).toBe("s-9");
    expect(out?.agreements.sections).toHaveLength(1);
    await waitFor(() => expect(result.current.data?.sections).toHaveLength(1));
  });

  // Which refusals get a named sentence, and -- separately -- which of them
  // refetch. AGREEMENT_SECTION_NAME_TAKEN deliberately does NOT: the modal
  // stays open with the typed name intact (spec, Error handling), and a
  // refetch there would be a request nobody needs. Anything that is not an
  // ApiError at all (a network fault, a Zod parse failure on an ok response)
  // takes the caller's own fallback.
  it("handleWriteError names each refusal, refetches only where the page is out of date, and falls back on anything else", () => {
    const reload = vi.fn(async () => {});

    expect(handleWriteError(new ApiError(409, "AGREEMENT_CHANGED", "…"), reload, "fallback"))
      .toBe(AGREEMENT_COPY.writeErrorChanged);
    expect(handleWriteError(new ApiError(409, "AGREEMENTS_NEED_TWO_OWNERS", "…"), reload, "fallback"))
      .toBe(AGREEMENT_COPY.writeErrorLocked);
    expect(handleWriteError(new ApiError(409, "AGREEMENT_PROPOSAL_RESOLVED", "…"), reload, "fallback"))
      .toBe(AGREEMENT_COPY.writeErrorResolved);
    expect(reload).toHaveBeenCalledTimes(3);

    expect(handleWriteError(new ApiError(409, "AGREEMENT_SECTION_NAME_TAKEN", "…"), reload, "fallback"))
      .toBe(AGREEMENT_COPY.sectionNameTaken);
    expect(handleWriteError(new ApiError(500, "INTERNAL", "…"), reload, "fallback")).toBe("fallback");
    expect(handleWriteError(new TypeError("network down"), reload, "fallback")).toBe("fallback");
    expect(reload).toHaveBeenCalledTimes(3);
  });
});
```

- [ ] **Step 2: Run them and watch them fail** — `cd web && npx vitest run src/features/marriage/useAgreements.test.ts`.

Expected: the suite never collects. Vite reports

```
Error: Failed to resolve import "./agreementCopy" from "src/features/marriage/useAgreements.test.ts". Does the file exist?
```

— it names the **first** of this file's four new modules in import order, so if you reordered the imports you will see `./agreementSchemas` or `./useAgreements` instead; any of the three is the same fact. This is a **build** failure, not a test failure: no test ran and no assertion was evaluated. Say that in the commit body.

- [ ] **Step 3: Write the key and the schemas.**

`web/src/features/marriage/agreementQueryKeys.ts`:

```ts
// The Agreements feature's TanStack Query key, in its own module for the
// reason visionQueryKeys.ts:1-4 records: AgreementsPage and the Retros page's
// To-discuss block read the same document, and neither should import the
// other just to invalidate a cache.
//
// No parameter. Every other query key in this codebase takes the thing it
// addresses (a year, a month, a goal id); this one addresses "the household's
// agreements", and the session already fixes the household -- a householdId
// parameter would be a value the browser holds and the server ignores.
export function agreementsQueryKey() {
  return ["agreements"] as const;
}
```

`web/src/features/marriage/agreementSchemas.ts`:

```ts
// Zod mirrors of the DTOs in api/internal/adapter/http/agreement_handlers.go
// (agreementDTO, agreementSectionDTO, agreementOwnerDTO, agreementProposalDTO,
// agreementHistoryEntryDTO, agreementsDocumentDTO and the three response
// envelopes). These follow the Go structs field for field rather than the
// design doc, the convention visionSchemas.ts:1-5 and retroSchemas.ts already
// use -- the Go comments are what say which fields can be meaningless and why.
import { z } from "zod";

// Mirrors domain.AgreementProposalKind. Three values and no default: a fourth
// kind needs a migration as well as a case, and anything this schema does not
// recognise must fail the parse rather than silently render as an "add".
export const agreementKindSchema = z.enum(["add", "edit", "remove"]);
export type AgreementKind = z.infer<typeof agreementKindSchema>;

// FOUR-valued, deliberately. The document only ever carries pending and
// parked (accepted and withdrawn are excluded in SQL), but a WRITE response
// carries the row at the status it now holds -- an Agree that completes a
// signing set answers "accepted", a withdraw answers "withdrawn". apiFetch
// turns a failed parse of an ok response into a thrown error, so a two-valued
// enum here would fail the very click that finished the agreement. Narrowing
// to pending/parked is ProposalCard's own switch (Task 12), never the
// schema's job.
export const agreementStatusSchema = z.enum(["pending", "parked", "accepted", "withdrawn"]);

// number is the design's "01" as an integer, composed by the service on every
// read and stored nowhere (spec decisions 10 and 11) -- the zero padding is
// the browser's, so this is a plain int and never a pre-padded string.
export const agreementSchema = z.object({
  id: z.string(),
  number: z.number().int(),
  body: z.string(),
});
export type Agreement = z.infer<typeof agreementSchema>;

// Every section travels, empty ones included: the page renders the visible
// ones and the Propose picker offers them all, off ONE array. count is its
// live agreements and visible is count > 0, both stamped by the service --
// the screen reads flags here, it does not re-derive the rule.
export const agreementSectionSchema = z.object({
  id: z.string(),
  name: z.string(),
  count: z.number().int(),
  visible: z.boolean(),
  agreements: z.array(agreementSchema),
});
export type AgreementSection = z.infer<typeof agreementSectionSchema>;

export const agreementOwnerSchema = z.object({
  membershipId: z.string(),
  name: z.string(),
});
export type AgreementOwner = z.infer<typeof agreementOwnerSchema>;

// proposedByName is "" when the membership no longer resolves -- an ordinary
// state for any household a partner has left, not a corruption, since the
// signature and proposal rows outlive the membership by design. The card
// drops attribution rather than printing an empty name.
//
// canAgree/canWithdraw say the caller MAY act, not that the write will
// succeed; freshness is targetChanged's job. All four are server-stamped, and
// nothing in the browser recomputes them from a membership id.
export const agreementProposalSchema = z.object({
  id: z.string(),
  kind: agreementKindSchema,
  status: agreementStatusSchema,
  sectionId: z.string(),
  sectionName: z.string(),
  targetAgreementId: z.string(),
  body: z.string(),
  previousBody: z.string(),
  note: z.string(),
  parkNote: z.string(),
  proposedByMembershipId: z.string(),
  proposedByName: z.string(),
  proposedAt: z.string(),
  awaitingNames: z.array(z.string()),
  targetChanged: z.boolean(),
  canAgree: z.boolean(),
  canWithdraw: z.boolean(),
});
export type AgreementProposal = z.infer<typeof agreementProposalSchema>;

// One accepted change. version is the one this change PRODUCED, and
// signedByNames is read from the signatures it collected, never from today's
// owners -- a signer whose membership no longer resolves is omitted from the
// list rather than joined as an empty string.
export const agreementHistoryEntrySchema = z.object({
  version: z.number().int(),
  proposalId: z.string(),
  kind: agreementKindSchema,
  sectionId: z.string(),
  sectionName: z.string(),
  body: z.string(),
  previousBody: z.string(),
  note: z.string(),
  proposedByName: z.string(),
  signedByNames: z.array(z.string()),
  acceptedAt: z.string(),
});
export type AgreementHistoryEntry = z.infer<typeof agreementHistoryEntrySchema>;

// Timestamps are z.string(): the wire carries RFC 3339 and only the three
// label helpers in agreementCopy.ts parse them, so a Date on the boundary
// would be a second representation nothing needs.
//
// updatedAt is null until something has been agreed -- rendered as an absent
// clause, never as "v1, updated —". Every array is [] on the wire and never
// null (the service builds each with make(..., 0, n)), so these are required
// arrays rather than optional ones with a default: a missing key means the
// server drifted, and that must fail the parse.
export const agreementsDocumentSchema = z.object({
  locked: z.boolean(),
  owners: z.array(agreementOwnerSchema),
  version: z.number().int(),
  updatedAt: z.string().nullable(),
  sections: z.array(agreementSectionSchema),
  proposals: z.array(agreementProposalSchema),
  history: z.array(agreementHistoryEntrySchema),
});
export type AgreementsDocument = z.infer<typeof agreementsDocumentSchema>;

// The three envelopes. Every 2xx on this feature carries a JSON body and every
// body is wrapped -- the read is { agreements }, a section write is
// { section, agreements } and the four proposal writes are
// { proposal, agreements }. The hook parses the envelope and hands its callers
// the inner object.
export const agreementsResponseSchema = z.object({ agreements: agreementsDocumentSchema });
export const agreementSectionWriteResponseSchema = z.object({
  section: agreementSectionSchema,
  agreements: agreementsDocumentSchema,
});
export const agreementProposalWriteResponseSchema = z.object({
  proposal: agreementProposalSchema,
  agreements: agreementsDocumentSchema,
});
```

`AgreementSection` is exported **here and nowhere else**. Task 11's `AgreementSectionCard.tsx` imports this type rather than declaring its own `AgreementsDocument["sections"][number]` alias — two modules exporting one name for one meaning is how the two drift apart.

- [ ] **Step 4: Write the copy module** — `web/src/features/marriage/agreementCopy.ts`, a plain `.ts` module for the reason `visionCopy.ts:1-5` gives (eslint's `react-refresh/only-export-components` never has to think about a file that mixes components with other exports, and every user-facing string lives in exactly one place).

The three helpers first:

```ts
// "Christine"; "Andreas and Christine"; "Andreas, Bev and Christine". One
// function for four call sites -- the proposal card's "needs …", version
// history's "Agreed by …", the propose modal's subtitle and the seeded
// sentence -- so three owners cannot read correctly on one screen and wrongly
// on the next. "and", not the design's "&": one caller joins section NAMES,
// and "Home & kids & Us" reads as three sections rather than two.
export function joinNames(names: string[]): string {
  if (names.length === 0) return "";
  if (names.length === 1) return names[0];
  return `${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
}

// "2026-06-28T21:18:52+08:00" -> "28 Jun", the design's header wording. The
// wire carries a full timestamp with its own offset, so none of monthNameOnly's
// UTC-midnight caution applies (retroCopy.ts:289-297 has that reasoning, for
// the "2026-06" date-only strings retros uses instead).
export function agreementDateLabel(iso: string): string {
  return new Date(iso).toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

// The same date with its year -- "28 Jun 2026". Version history spans years by
// construction (nothing is ever deleted, so the log only grows), while a
// pending proposal is days old; two helpers rather than one flag, so neither
// call site has to remember which boolean means "with year".
export function historyDateLabel(iso: string): string {
  return new Date(iso).toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}
```

Then the one copy object. **There is exactly one copy object in this feature.** Tasks 11–16 append their keys to `AGREEMENT_COPY` under their own banner comment, the way `visionCopy.ts:67` opens its `VisionModal` block — no `AGREEMENT_DOCUMENT_COPY`, no `PROPOSAL_CARD_COPY`, no second module. A duplicate key inside one object literal is a TypeScript error, so the reserved names are listed here rather than left to be discovered:

```ts
// Every user-visible string on the Agreements screen, its three modals and the
// Retros To-discuss block. ONE object: later tasks append keys under their own
// banner, they do not open a second export.
//
// Already taken by this task, and not to be re-added under another name:
//   subtitle, versionClause  -- Task 11 renders the same subtitle element this
//                               task built; it must not add `versionSuffix`
//                               or an AGREEMENT_DOCUMENT_COPY of its own.
//   useStarterSet            -- not `starterSet`.
//   seededHeadline, seededBody -- not `sectionsReadyTitle`/`sectionsReadyBody`.
//   sectionNameTaken         -- Task 14 reuses this one; it does not redeclare it.
// Reserved for Task 12, which appends them HERE: agree, discuss, withdraw,
// everyoneAgreed, pendingTitle, parkedTitle, cancel, parkNoteLabel.
export const AGREEMENT_COPY = {
  // --- The page, in every state (Task 10) --------------------------------
  title: "Our agreements",
  subtitle: "A living document — changes need every owner to agree",
  // Rendered in every state, including the populated one the design drops it
  // from: a household reading a page of private promises should be told it is
  // private on the screen that holds the most of them.
  privacyBadge: "🔒 Private — parents only",
  // Appended to the subtitle only once updatedAt is a real moment, never
  // "v1, updated —". Takes an already-formatted date rather than an ISO
  // string, so the caller chooses which of the two date labels applies.
  versionClause: (version: number, updated: string) => `v${version}, updated ${updated}`,
  loading: "Loading…",
  // Task 14 renders the button this names, in both unlocked empty states. The
  // key lands here because every string in this feature lives in one object.
  addFirstAgreement: "Add your first agreement",

  loadError: "Couldn't load your agreements.",
  // The routine "not the owner" refusal, told plainly and never as a red
  // alert (docs/LEARNING.md pattern 1) -- the exact gap BillsPage.tsx shipped
  // without, found again in BudgetPage.tsx and TransactionsPage.tsx.
  ownerOnlyHeading: "Owner only",
  ownerOnlyBody:
    "Agreements are visible to the household owner. Ask them if you'd like to see where things stand.",

  // The three header controls. Hidden -- not disabled -- when the document is
  // locked, except Version history, which stays because it is a read
  // (spec decision 3).
  newSection: "+ Section",
  versionHistory: "Version history",
  proposeChange: "Propose a change",

  // --- Locked, nothing written yet (decision 2) --------------------------
  lockedTile: "🤝",
  // Never "both of you": the signing set is every CURRENT owner (decision 4),
  // and nothing in this product caps a household at two.
  lockedHeadline: "Agreements need at least two owners",
  lockedBody:
    "Agreements are the promises your household lives by. Every one of them is agreed by all the owners before it takes effect.",
  lockedOwnerCount: "This household has one owner.",
  invitePartner: "Invite your partner",

  // --- Locked, with content (decision 3) ---------------------------------
  frozenBanner:
    "This household is down to one owner, so nothing here can change. Everything you agreed is still here, and it unlocks again when a second owner joins.",
  // So a household can see its frozen proposals were not deleted. The verb
  // agrees with the count, the way the Retros subtitle's already does.
  frozenProposals: (count: number) =>
    count === 1 ? "1 change is waiting for a second owner" : `${count} changes are waiting for a second owner`,

  // --- Empty, no sections ------------------------------------------------
  emptyHeadline: "Write your first agreements",
  emptyBody:
    "Agreements are the promises you keep to each other — how you handle money, conflict, home and time together. Each one is proposed by one owner and takes effect once every owner agrees.",
  useStarterSet: "Use starter set",
  popularHeading: "Popular starting points",
  // Read-only illustration. The design draws these four with hover styling and
  // no onClick, and nothing says what tapping one would propose -- so the
  // drawing ships and the interaction does not (spec, Out of scope). Held here
  // rather than as literals in the page so the four labels have one home.
  popularCards: ["Money", "Conflict", "Home & kids", "Us"],
  starterSetError: "Couldn't add the starter sections. Try again.",

  // --- Empty, sections seeded (decisions 8, 17) --------------------------
  // An empty section is invisible in the document, so a naively rendered
  // starter set changes nothing on screen. Naming the four in prose is what
  // proves the click worked, without needing an exception to decision 8.
  seededHeadline: "Your sections are ready",
  seededBody: (names: string[]) =>
    `${joinNames(names)} are ready. Nothing has been agreed yet — every agreement is proposed by one owner and takes effect once every owner agrees.`,

  // --- Write refusals, shown by handleWriteError (Task 9) ----------------
  writeErrorChanged:
    "This no longer matches the agreement it was written against, so nothing was signed.",
  writeErrorLocked: "This household is down to one owner, so nothing here can change.",
  writeErrorResolved: "That was already settled — this page has just refreshed.",
  sectionNameTaken: "You already have a section called that.",
} as const;
```

- [ ] **Step 5: Write the hook** — `web/src/features/marriage/useAgreements.ts`, in full:

```ts
// Fetch orchestration for the Agreements document: one GET, six writes, and
// the one query key both screens read -- AgreementsPage and the Retros page's
// To-discuss block, which owns its own useAgreements() call the way VisionCard
// owns its own useVision().
//
// Every write invalidates that key rather than trusting its own response. Each
// response does carry the whole freshly composed document, but the service
// composes it AFTER its transaction commits and outside it, so a concurrent
// Agree may already have overtaken the snapshot -- the refetch stays the
// authority. Invalidating is also what makes one write refresh BOTH mounted
// screens: they share the key rather than being told about each other.
//
// Returned field names are TanStack's own (isLoading, isProposing,
// isCreatingSection) rather than useVision.ts's `loading`, because six
// mutations sit beside the query here and one naming family across all seven
// flags is easier to read than two.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, apiFetch } from "../../api/client";
import { AGREEMENT_COPY } from "./agreementCopy";
import { agreementsQueryKey } from "./agreementQueryKeys";
import {
  agreementProposalWriteResponseSchema,
  agreementSectionWriteResponseSchema,
  agreementsResponseSchema,
  type AgreementKind,
  type AgreementSection,
  type AgreementsDocument,
} from "./agreementSchemas";

const BASE = "/api/v1/marriage/agreements";

// Mirrors proposeAgreementChangeRequest (agreement_handlers.go). Every field is
// always sent, empty where the kind does not use it: the handler blanks
// sectionId itself for an edit or a remove, and an OMITTED previousBody would
// always read as stale, since an agreement body is never empty.
export type ProposeBody = {
  kind: AgreementKind;
  sectionId: string;
  targetAgreementId: string;
  body: string;
  previousBody: string;
  note: string;
};

async function fetchAgreements(): Promise<AgreementsDocument> {
  const raw = await apiFetch<unknown>(BASE);
  return agreementsResponseSchema.parse(raw).agreements;
}

// The four proposal routes share one envelope and each answers the whole
// document: every one of them moves the version, the 01..N numbering, the
// history list and which proposals are open -- not only the row it touched.
// The `proposal` half of the envelope is parsed (so a drifted status fails
// loudly here rather than three screens later) and then dropped: no component
// in this feature renders it.
async function postProposalAction(path: string, body?: unknown): Promise<AgreementsDocument> {
  const raw = await apiFetch<unknown>(path, {
    method: "POST",
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
  });
  return agreementProposalWriteResponseSchema.parse(raw).agreements;
}

// One failed write -> the sentence to show, plus a refetch wherever the answer
// is "the page you clicked is out of date". It calls reload, so it is
// behaviour and lives here rather than in agreementCopy.ts; `fallback` is each
// caller's own copy for a genuine server failure.
//
// AGREEMENT_SECTION_NAME_TAKEN deliberately does NOT refetch: the New-section
// modal stays open with the typed name intact, and nothing about the document
// changed.
//
// ProposalCard keeps no conflict latch: it holds no draft, and the refetch
// brings back targetChanged: true, which disables Agree for every owner from
// then on. The latch belongs to ProposeAgreementModal (Task 13), which does
// hold one.
export function handleWriteError(
  err: unknown,
  reload: () => Promise<void>,
  fallback: string,
): string {
  if (!(err instanceof ApiError)) return fallback;
  switch (err.code) {
    case "AGREEMENT_CHANGED":
      // Not awaited: this function answers with a sentence synchronously, and
      // the refetch it starts lands through the query cache like any other.
      void reload();
      return AGREEMENT_COPY.writeErrorChanged;
    case "AGREEMENTS_NEED_TWO_OWNERS":
      // An owner removed between paint and click. The refetch flips the page
      // to the read-only locked document, which explains the state far better
      // than a line under one button.
      void reload();
      return AGREEMENT_COPY.writeErrorLocked;
    case "AGREEMENT_PROPOSAL_RESOLVED":
      void reload();
      return AGREEMENT_COPY.writeErrorResolved;
    case "AGREEMENT_SECTION_NAME_TAKEN":
      return AGREEMENT_COPY.sectionNameTaken;
    default:
      return fallback;
  }
}

export function useAgreements() {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: agreementsQueryKey(), queryFn: fetchAgreements });

  // RETURNED, not fired and forgotten: TanStack awaits whatever onSuccess
  // returns before the mutation settles, so a caller's await lands on a
  // refreshed cache rather than racing it.
  const afterWrite = () => queryClient.invalidateQueries({ queryKey: agreementsQueryKey() });

  const createSectionMutation = useMutation({
    mutationFn: async (
      name: string,
    ): Promise<{ section: AgreementSection; agreements: AgreementsDocument }> => {
      const raw = await apiFetch<unknown>(`${BASE}/sections`, {
        method: "POST",
        body: JSON.stringify({ name }),
      });
      // Both halves, unlike every other write: Task 14's "Create & add first
      // agreement" seeds the Propose modal with the new section's id.
      return agreementSectionWriteResponseSchema.parse(raw);
    },
    onSuccess: afterWrite,
  });

  const starterSetMutation = useMutation({
    mutationFn: async (): Promise<AgreementsDocument> => {
      // No body, and 200 rather than 201: the starter set is idempotent and a
      // second click may create nothing, so it answers the plain document
      // envelope rather than a write envelope with no row to name.
      const raw = await apiFetch<unknown>(`${BASE}/starter-set`, { method: "POST" });
      return agreementsResponseSchema.parse(raw).agreements;
    },
    onSuccess: afterWrite,
  });

  const proposeMutation = useMutation({
    mutationFn: (body: ProposeBody) => postProposalAction(`${BASE}/proposals`, body),
    onSuccess: afterWrite,
  });

  const agreeMutation = useMutation({
    // Agree and withdraw send no body: a client-echoed previousBody would be
    // the one copy nobody verified.
    mutationFn: (id: string) =>
      postProposalAction(`${BASE}/proposals/${encodeURIComponent(id)}/agree`),
    onSuccess: afterWrite,
  });

  const parkMutation = useMutation({
    mutationFn: (v: { id: string; note: string }) =>
      postProposalAction(`${BASE}/proposals/${encodeURIComponent(v.id)}/park`, { note: v.note }),
    onSuccess: afterWrite,
  });

  const withdrawMutation = useMutation({
    mutationFn: (id: string) =>
      postProposalAction(`${BASE}/proposals/${encodeURIComponent(id)}/withdraw`),
    onSuccess: afterWrite,
  });

  return {
    data: query.data,
    // v5's isLoading is `isPending && isFetching` -- true only while the first
    // fetch for this key is in flight with no cached value, so the background
    // refetch after a write does not blank the screen.
    isLoading: query.isLoading,
    error: query.error,
    reload: async () => {
      await query.refetch();
    },
    createSection: (name: string) => createSectionMutation.mutateAsync(name),
    seedStarterSet: () => starterSetMutation.mutateAsync(),
    propose: (body: ProposeBody) => proposeMutation.mutateAsync(body),
    agree: (proposalId: string) => agreeMutation.mutateAsync(proposalId),
    // Two arguments at the call site, one object on the wire: a card calls
    // park(id, note) and does not have to know this mutation takes a pair.
    park: (proposalId: string, note: string) => parkMutation.mutateAsync({ id: proposalId, note }),
    withdraw: (proposalId: string) => withdrawMutation.mutateAsync(proposalId),
    // The two in-flight flags a modal needs to disable its own submit button
    // (Tasks 13 and 14). The four proposal-card writes each own their own
    // in-flight pair locally, per card, since one shared flag would disable
    // every card's buttons whenever any one of them was mid-write.
    isProposing: proposeMutation.isPending,
    isCreatingSection: createSectionMutation.isPending,
  };
}
```

- [ ] **Step 6: Run the tests and watch them pass** — `cd web && npx vitest run src/features/marriage/useAgreements.test.ts`. Expected: PASS, 3 tests.

- [ ] **Step 7: Mutation-check the four-valued status, and the refusal that must not refetch.**

  1. In `agreementSchemas.ts`, narrow `agreementStatusSchema` to `z.enum(["pending", "parked"])`. Expected: **"parses the accepted proposal an Agree answers with, and invalidates the one key both screens read"** goes red — `agree("p-1")` rejects with a `ZodError` on `proposal.status` (`Invalid option: expected one of "pending"|"parked"`) surfacing out of `act`. A **test** failure, and the other two tests stay green: neither parses a write response's proposal. Restore.
  2. In `useAgreements.ts`, add `void reload();` to `handleWriteError`'s `AGREEMENT_SECTION_NAME_TAKEN` arm. Expected: **"handleWriteError names each refusal, refetches only where the page is out of date, and falls back on anything else"** goes red on the last assertion — `expected "spy" to be called 3 times, but got 4 times`. A **test** failure. Restore.

- [ ] **Step 8: Commit**

```bash
cd web && npx vitest run src/features/marriage && cd .. && make lint
git add web/src/features/marriage/agreementQueryKeys.ts web/src/features/marriage/agreementSchemas.ts \
        web/src/features/marriage/agreementCopy.ts web/src/features/marriage/useAgreements.ts \
        web/src/features/marriage/useAgreements.test.ts
git commit -m "feat(agreements): one query key, a four-valued status, six writes behind one invalidation

Two mutation checks, both TEST failures (not build failures):

1. Narrowed agreementStatusSchema to [pending, parked]. The agree test went
   red with a ZodError on proposal.status; the createSection and
   handleWriteError tests stayed green, neither parsing a write response's
   proposal row.
2. Added a reload() call to handleWriteError's AGREEMENT_SECTION_NAME_TAKEN
   arm. The handleWriteError test went red on its final call count (4, want
   3) -- the New-section modal keeps the typed name and must not refetch."
```

---

### Task 10: The page, its four pre-document states, the three header controls, and the way in

**Files:**

- Create `web/src/features/marriage/AgreementsPage.tsx`, `web/src/features/marriage/AgreementsPage.test.tsx`, `web/src/features/marriage/agreementFixtures.tsx`.
- Modify `web/src/features/marriage/useAgreements.test.ts` (delete its four local fixtures, import them from `./agreementFixtures`).
- Modify `web/src/routes/router.tsx` (the feature import beside `:97`; the new route after `marriageVisionRoute` ends at `:303`; `settingsRoute` at `:319-326`; the children list at `:667-671`) and `web/src/routes/router.test.tsx` (two new tests beside the Vision route test at `:558`).
- Modify `web/src/features/shell/Sidebar.tsx` (`SPACE_PAGES.marriage`, `:45-54`) and `web/src/features/shell/Sidebar.test.tsx` (`:137`, `:154`).
- Modify `web/src/features/settings/SettingsPage.tsx` (`:12`, `:31`), `web/src/features/settings/MembersPanel.tsx` (`:229`, `:233`) and `web/src/features/settings/MembersPanel.test.tsx`.

**Interfaces:**

Consumes, from Task 9 — copy these verbatim, invent nothing:

```ts
useAgreements(): {
  data: AgreementsDocument | undefined; isLoading: boolean; error: unknown
  reload: () => Promise<void>
  createSection: (name: string) => Promise<{ section: AgreementSection; agreements: AgreementsDocument }>
  seedStarterSet: () => Promise<AgreementsDocument>
  propose: (body: ProposeBody) => Promise<AgreementsDocument>
  agree: (proposalId: string) => Promise<AgreementsDocument>
  park: (proposalId: string, note: string) => Promise<AgreementsDocument>
  withdraw: (proposalId: string) => Promise<AgreementsDocument>
  isProposing: boolean; isCreatingSection: boolean
}
handleWriteError(err: unknown, reload: () => Promise<void>, fallback: string): string
AGREEMENT_COPY, agreementDateLabel(iso: string): string
type AgreementsDocument, AgreementKind
```

Also consumes `useMe()` from `web/src/features/auth/useAuth.ts:27`.

Produces:

```tsx
// AgreementsPage.tsx
// No seed type is declared or exported here. The page holds one in state, but
// the type belongs to ProposeAgreementModal.tsx, which Task 13 creates: the
// page imports the modal, so the modal must not import the page back. Until
// that file exists, this useState carries the shape inline.
export function AgreementsPage(): JSX.Element

// agreementFixtures.tsx -- the shared test helpers every later frontend task reuses
export const DOC_URL: string                 // "/api/v1/marriage/agreements"
export const ME_URL: string                  // "/api/v1/auth/me"
export const OWNERS: AgreementOwner[]        // Andreas (m-1) and Christine (m-2)
export const ONE_OWNER: AgreementOwner[]     // Andreas alone
export const STARTER_SECTION_NAMES: string[]
export function meFixture(overrides?: Partial<Me>): Me
export function documentFixture(o?: Partial<AgreementsDocument>): AgreementsDocument  // INNER
export function proposalFixture(o?: Partial<AgreementProposal>): AgreementProposal    // INNER
export function emptySection(name: string): AgreementSection                          // INNER
export function emptyDoc(o?: Partial<AgreementsDocument>): { agreements: AgreementsDocument }   // WRAPPED
export function seededDoc(o?: Partial<AgreementsDocument>): { agreements: AgreementsDocument }  // WRAPPED
export function renderPage(routes?: Record<string, RouteResponse | RouteResponse[]>): …
```

Produces, in the app: the route `/marriage/agreements`; the Sidebar entry; `settingsRoute`'s `validateSearch: (search) => { invite?: true }`; `SettingsPage({ openInvite }: { openInvite?: boolean })`; `MembersPanel({ openInvite }: { openInvite?: boolean })`.

Produces, as a contract Tasks 11–15 render against — these `data-testid`s exist after this task and must survive it:

| `data-testid` | Where |
|---|---|
| `agreements-page` | the `PageContainer`, present in all four data states |
| `agreements-subtitle` | the header's `<p>`, holding the subtitle and the version clause |
| `agreements-privacy-badge` | the header's static badge |
| `agreements-new-section`, `agreements-history`, `agreements-propose` | the three header controls |
| `agreements-owner-only`, `agreements-load-error` | the two error branches (neither inside `agreements-page`) |
| `agreements-locked-invite`, `agreements-frozen`, `agreements-empty`, `agreements-seeded` | the four pre-document states |
| `agreements-starter-set` | the "Use starter set" button |

**Task 11 mounts the sections grid as a new child of `agreements-page`, for every state except `agreements-locked-invite`** — a locked household with content still sees its document (decision 3), and an unlocked empty or seeded household renders a grid with nothing visible in it. Task 11 must not replace the header, the subtitle element or its `data-testid`, and must not add a second copy object: `AGREEMENT_COPY.subtitle` and `AGREEMENT_COPY.versionClause` are already rendered here.

Two rules bite in this task. **The 403 branch is a plain `<section>`; only a genuine failure is `role="alert" … text-danger`** — `RetrosPage.tsx:79-88` and `VisionPage.tsx:64-75` verbatim, branching on the real status and never on a second `useMe()` role check that could disagree with the server (`docs/LEARNING.md` pattern 1). And **nothing renders from a guess**: never `data?.locked ?? true`, which flashes "you need a second owner" at a two-owner household on every cold load.

- [ ] **Step 1: Write the shared fixtures, then the failing page tests.**

`web/src/features/marriage/agreementFixtures.tsx` — test support, not production code, in the same shape as `web/src/test/renderWithRouter.tsx` (a non-`.test` module under `src` that imports Testing Library, so several test files can share one harness):

```tsx
// The Agreements feature's shared test fixtures. Every frontend task from here
// on (11-16) builds its stubs from these rather than retyping a seventeen-field
// proposal, so one field renamed on the wire is one edit here, not seven.
//
// WRAPPED vs INNER matters and is stated on each export: the API wraps every
// body ({ agreements } for a read, { section, agreements } and
// { proposal, agreements } for the writes), so documentFixture/proposalFixture
// return the INNER object a component sees, while emptyDoc/seededDoc return the
// ENVELOPE, because they are passed straight into a stub's `body:`.
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { renderWithRouter } from "../../test/renderWithRouter";
import type { Me } from "../auth/schemas";
import { AgreementsPage } from "./AgreementsPage";
import type {
  AgreementOwner,
  AgreementProposal,
  AgreementSection,
  AgreementsDocument,
} from "./agreementSchemas";

export const DOC_URL = "/api/v1/marriage/agreements";
export const ME_URL = "/api/v1/auth/me";

// m-1 is also meFixture's membership id below, so the viewer of every page
// test is Andreas -- the first owner and, in proposalFixture, the proposer.
export const OWNERS: AgreementOwner[] = [
  { membershipId: "m-1", name: "Andreas" },
  { membershipId: "m-2", name: "Christine" },
];
export const ONE_OWNER: AgreementOwner[] = [{ membershipId: "m-1", name: "Andreas" }];

// domain.StarterSectionNames(), mirrored: "Use starter set" seeds these four
// labels and no agreements (decision 17).
export const STARTER_SECTION_NAMES = ["Money", "Conflict", "Home & kids", "Us"];

export function meFixture(overrides: Partial<Me> = {}): Me {
  return {
    user: {
      id: "u-andreas",
      email: "andreas@hearth.family",
      displayName: "Andreas",
      avatarInitial: "A",
    },
    household: {
      id: "h-1",
      name: "Andreas & Christine",
      familyName: "Oentoro",
      primaryCurrency: "SGD",
      showSecondaryCurrency: false,
      secondaryCurrency: "",
      fxRateMode: "static",
    },
    membership: {
      id: "m-1",
      householdId: "h-1",
      userId: "u-andreas",
      role: "owner",
      capabilities: ["calendar", "chores", "money", "marriage"],
    },
    capabilities: ["calendar", "chores", "money", "marriage"],
    spaces: [],
    isPlatformAdmin: false,
    features: {},
    ...overrides,
  };
}

// INNER. Two owners, so the default document is unlocked; a locked fixture
// passes { locked: true, owners: ONE_OWNER } explicitly, because the two travel
// together on the wire and a fixture that set only one of them would be a shape
// the server cannot produce.
export function documentFixture(o: Partial<AgreementsDocument> = {}): AgreementsDocument {
  return {
    locked: false,
    owners: OWNERS,
    version: 1,
    updatedAt: null,
    sections: [],
    proposals: [],
    history: [],
    ...o,
  };
}

// INNER. proposedAt/updatedAt carry a real offset: 2026-06-28T21:18:52+08:00 is
// 13:18 UTC, so agreementDateLabel reads "28 Jun" in every timezone from UTC-13
// to UTC+10 -- the date assertions below do not depend on the runner's TZ.
export function proposalFixture(o: Partial<AgreementProposal> = {}): AgreementProposal {
  return {
    id: "p-1",
    kind: "add",
    status: "pending",
    sectionId: "s-1",
    sectionName: "Money",
    targetAgreementId: "",
    body: "One shared account for bills",
    previousBody: "",
    note: "",
    parkNote: "",
    proposedByMembershipId: "m-1",
    proposedByName: "Andreas",
    proposedAt: "2026-09-05T10:00:00+08:00",
    awaitingNames: ["Christine"],
    targetChanged: false,
    canAgree: true,
    canWithdraw: false,
    ...o,
  };
}

// INNER. A section with nothing agreed in it: count 0 and visible false, which
// is what the server stamps (decision 8) and what makes a seeded starter set
// change nothing on screen unless the page says so in prose.
export function emptySection(name: string): AgreementSection {
  return { id: `s-${name}`, name, count: 0, visible: false, agreements: [] };
}

// WRAPPED -- goes straight into a stub's `body:`. An unlocked household with no
// sections at all: the "Write your first agreements" state.
export function emptyDoc(o: Partial<AgreementsDocument> = {}): { agreements: AgreementsDocument } {
  return { agreements: documentFixture(o) };
}

// WRAPPED. Where "Use starter set" lands: four labels, no agreements.
export function seededDoc(o: Partial<AgreementsDocument> = {}): { agreements: AgreementsDocument } {
  return { agreements: documentFixture({ sections: STARTER_SECTION_NAMES.map(emptySection), ...o }) };
}

// Mounts AgreementsPage with GET /auth/me already stubbed. The page calls
// useMe(), and stubFetchRoutes THROWS on an unregistered request -- so a test
// that forgot the session stub would fail on a missing route rather than on
// what it meant to assert. Caller routes are spread last, so a test that needs
// a different member (Task 13's co-owner names) overrides the same key.
export function renderPage(routes: Record<string, RouteResponse | RouteResponse[]> = {}) {
  const stub = stubFetchRoutes({
    [`GET ${ME_URL}`]: { status: 200, body: meFixture() },
    ...routes,
  });
  return { stub, ...renderWithRouter(<AgreementsPage />) };
}
```

Then `web/src/features/marriage/AgreementsPage.test.tsx`:

```tsx
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { AgreementsPage } from "./AgreementsPage";
import {
  DOC_URL,
  ONE_OWNER,
  documentFixture,
  emptyDoc,
  emptySection,
  proposalFixture,
  renderPage,
  seededDoc,
} from "./agreementFixtures";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("AgreementsPage", () => {
  it("a limited member is told this is owner-only, not that something broke", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 403,
        body: { error: { code: "FORBIDDEN", message: "Owner only." } },
      },
    });

    expect(await screen.findByTestId("agreements-owner-only")).toHaveTextContent("Owner only");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("a non-403 failure renders the alert, not the owner-only explanation", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 500,
        body: { error: { code: "INTERNAL", message: "Broke." } },
      },
    });

    expect(await screen.findByTestId("agreements-load-error")).toHaveTextContent(
      "Couldn't load your agreements.",
    );
    expect(screen.queryByTestId("agreements-owner-only")).not.toBeInTheDocument();
  });

  // Decision 2. A household that has never had two owners has nothing to show
  // and no history to read, so all three header controls are absent -- there is
  // no version to look back at and nothing that could be proposed.
  it("a household that has never had two owners is offered the invite deep link and no write controls", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 200,
        body: emptyDoc({ locked: true, owners: ONE_OWNER }),
      },
    });

    expect(await screen.findByTestId("agreements-locked-invite")).toHaveTextContent(
      "This household has one owner.",
    );
    expect(screen.getByRole("link", { name: "Invite your partner" })).toHaveAttribute(
      "href",
      "/settings?invite=true",
    );
    expect(screen.queryByTestId("agreements-frozen")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agreements-propose")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agreements-new-section")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agreements-history")).not.toBeInTheDocument();
  });

  // Decision 3: the promise two people made does not stop existing when one of
  // them leaves. A live agreement proves this household once had two owners;
  // sections alone would not, being labels rather than promises (decision 8).
  //
  // The spec's own words for this state: "Version history stays, being a read;
  // + Section, Propose a change ... are GONE, not disabled" -- so this asserts
  // absence, which a disabled-button implementation would fail.
  it("a locked household with content keeps its document, names its frozen proposals, and keeps only Version history", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 200,
        body: {
          agreements: documentFixture({
            locked: true,
            owners: ONE_OWNER,
            version: 2,
            updatedAt: "2026-06-28T21:18:52+08:00",
            proposals: [proposalFixture()],
            sections: [
              {
                ...emptySection("Money"),
                count: 1,
                visible: true,
                agreements: [{ id: "a-1", number: 1, body: "One shared account for bills" }],
              },
            ],
          }),
        },
      },
    });

    expect(await screen.findByTestId("agreements-frozen")).toHaveTextContent(
      "1 change is waiting for a second owner",
    );
    expect(screen.queryByTestId("agreements-locked-invite")).not.toBeInTheDocument();
    expect(screen.getByTestId("agreements-subtitle")).toHaveTextContent("v2, updated 28 Jun");
    expect(screen.getByTestId("agreements-history")).toBeInTheDocument();
    expect(screen.queryByTestId("agreements-propose")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agreements-new-section")).not.toBeInTheDocument();
  });

  // The first render passes vacuously once the query has answered, so this
  // holds the response open: the only way to see what a cold load actually
  // paints. renderPage is deliberately NOT used -- this test replaces fetch
  // wholesale so that /auth/me hangs too.
  it("says nothing about a second owner while the query is still in flight", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>(() => {})),
    );

    renderWithRouter(<AgreementsPage />);

    expect(await screen.findByText("Loading…")).toBeInTheDocument();
    expect(screen.queryByTestId("agreements-locked-invite")).not.toBeInTheDocument();
    expect(screen.queryByTestId("agreements-page")).not.toBeInTheDocument();
  });

  it("an unlocked household with no sections gets the empty state and all three header controls", async () => {
    renderPage({ [`GET ${DOC_URL}`]: { status: 200, body: emptyDoc() } });

    expect(await screen.findByTestId("agreements-empty")).toHaveTextContent(
      "Write your first agreements",
    );
    expect(screen.getByTestId("agreements-empty")).toHaveTextContent("Popular starting points");
    expect(screen.getByRole("button", { name: "+ Section" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Version history" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Propose a change" })).toBeInTheDocument();
    // The version clause is absent until updatedAt is a real moment -- never
    // "v1, updated —".
    expect(screen.getByTestId("agreements-subtitle")).not.toHaveTextContent("v1");
  });

  // Decision 8 makes an empty section invisible, so a naively rendered starter
  // set changes nothing on screen. This catches a button that appears to do
  // nothing -- browser criterion 4, pinned here as well because a walk that
  // navigates and waits would miss it too.
  it("Use starter set lands on the seeded state, which names the four sections", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: [
        { status: 200, body: emptyDoc() },
        { status: 200, body: seededDoc() },
      ],
      [`POST ${DOC_URL}/starter-set`]: { status: 200, body: seededDoc() },
    });

    fireEvent.click(await screen.findByRole("button", { name: "Use starter set" }));

    await waitFor(() => expect(screen.getByTestId("agreements-seeded")).toBeInTheDocument());
    expect(screen.getByTestId("agreements-seeded")).toHaveTextContent(
      "Money, Conflict, Home & kids and Us are ready.",
    );
    expect(screen.queryByRole("button", { name: "Use starter set" })).not.toBeInTheDocument();
  });
});
```

Finally, delete the four local fixtures from `web/src/features/marriage/useAgreements.test.ts` (`DOC_URL`, `OWNERS`, `documentFixture`, `proposalFixture`) and import them instead:

```ts
import { DOC_URL, documentFixture, proposalFixture } from "./agreementFixtures";
```

`renderUseAgreements` stays in that file: it renders a hook, not the page.

- [ ] **Step 2: Run them and watch them fail** — `cd web && npx vitest run src/features/marriage`.

Expected: neither Agreements file collects, because `agreementFixtures.tsx` imports a module that does not exist yet. Vite reports

```
Error: Failed to resolve import "./AgreementsPage" from "src/features/marriage/agreementFixtures.tsx". Does the file exist?
```

A **build** failure — no test ran. `useAgreements.test.ts` fails the same way now that it imports the fixtures module. Say which you saw in the commit body.

- [ ] **Step 3: Write the page** — `web/src/features/marriage/AgreementsPage.tsx`, in full:

```tsx
// The Agreements screen: the header every state shares, the four states that
// come before there is a document to render, and the page state Tasks 11-15
// mount against. Composition only, the RetrosPage.tsx/VisionPage.tsx
// convention -- fetch orchestration lives in useAgreements.ts and no apiFetch
// call belongs here.
//
// This page owns EVERY piece of state the feature's modals need: the session,
// the three modal slots and the three header buttons that fill them. A modal
// that owned its own open flag would still need a button somewhere else to set
// it, and the button and the flag would then live in two files. Tasks 13-15
// each add one modal and bind one already-existing value.
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { ApiError } from "../../api/client";
import { PageContainer } from "../../components/PageContainer";
import { useMe } from "../auth/useAuth";
import { AGREEMENT_COPY, agreementDateLabel } from "./agreementCopy";
import type { AgreementKind } from "./agreementSchemas";
import { handleWriteError, useAgreements } from "./useAgreements";

// One seed for all four of the Propose modal's entry points -- the header
// button, the New-section modal's "Create & add first agreement", a section
// card's own add, and version history's Restore. Every field is a pre-fill,
// so the modal never has to know which door the caller came through. `mode`
// reuses the wire's own kind enum rather than restating the union: two literal
// unions for one server enum is how the two drift apart.
//
// Written inline rather than as an exported type, because the name belongs to
// ProposeAgreementModal.tsx (Task 13) and this file already imports that one:
// exporting a second name here would either duplicate the union or make the
// modal import the page back. Task 13 replaces this annotation with the
// imported `AgreementProposeSeed`.

const PANEL = "rounded-xl border border-hairline bg-card p-[22px]";
// min-h-11 is the 44px touch-target floor (CLAUDE.md); inline-flex
// items-center because a <a> renders inline and would otherwise pin its text
// to the top of the grown box, the same reason Sidebar.tsx's NAV_ITEM_CLASS
// gives.
const CTA =
  "inline-flex min-h-11 items-center rounded-lg bg-accent px-3.5 text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60";
const HEADER_BUTTON =
  "inline-flex min-h-11 items-center rounded-lg border border-hairline bg-card px-3.5 text-[13px] text-ink";
const MUTED = "mt-1.5 text-[13px] text-muted";

export function AgreementsPage() {
  const agreements = useAgreements();
  // The page owns the session. Task 13's propose modal names the co-owners who
  // will be asked to agree ("Christine will be asked to agree before it takes
  // effect"), which needs both the owners list and which of them is the
  // viewer; a useMe() inside the modal would be a second subscriber to the
  // same query for one sentence.
  const me = useMe();

  // The three modal slots. The VALUES are elided here because nothing reads
  // them until their modal exists -- tsconfig has noUnusedLocals, so a bound
  // name with no reader would not compile. Each task below binds its own:
  //   Task 13 -> const [proposeSeed, setProposeSeed]
  //   Task 14 -> const [newSectionOpen, setNewSectionOpen]
  //   Task 15 -> const [historyOpen, setHistoryOpen]
  // and mounts its modal at the marked point at the bottom of this file. The
  // buttons, the state and its setters land here so a modal task adds a modal
  // and nothing else.
  const [, setProposeSeed] = useState<{
    mode: AgreementKind;
    sectionId?: string;
    targetAgreementId?: string;
    body?: string;
  } | null>(null);
  const [, setNewSectionOpen] = useState(false);
  const [, setHistoryOpen] = useState(false);

  // The one write this task makes. One flag and one error slot, not a
  // per-button pair: there is exactly one starter-set button on screen at a
  // time (RetrosPage.tsx's own precedent for its single Start control).
  const [seeding, setSeeding] = useState(false);
  const [seedError, setSeedError] = useState<string | null>(null);

  function handleStarterSet() {
    setSeeding(true);
    setSeedError(null);
    agreements
      .seedStarterSet()
      .catch((err: unknown) =>
        setSeedError(handleWriteError(err, agreements.reload, AGREEMENT_COPY.starterSetError)),
      )
      .finally(() => setSeeding(false));
  }

  // Both queries. The header this page paints carries a control that opens a
  // modal built from the session (Task 13), so painting it before /auth/me has
  // answered would offer a button whose modal has no names in it.
  if (agreements.isLoading || me.isLoading) {
    return <p className="p-9 text-xs text-muted">{AGREEMENT_COPY.loading}</p>;
  }

  if (agreements.error) {
    // The real status, never a second useMe() role check that could disagree
    // with what the server decided. Being told a screen is owner-only is not an
    // incident, so this half is a plain <section> and only the half below it is
    // an alert -- the gap BillsPage.tsx shipped without, docs/LEARNING.md
    // pattern 1, and the shape RetrosPage.tsx:79-88 already carries.
    const status = agreements.error instanceof ApiError ? agreements.error.status : undefined;
    if (status === 403) {
      return (
        <section data-testid="agreements-owner-only" className={`m-9 ${PANEL}`}>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">
            {AGREEMENT_COPY.title}
          </h1>
          <h2 className="mt-4 text-xs text-muted">{AGREEMENT_COPY.ownerOnlyHeading}</h2>
          <p className="mt-1.5 text-[13px] text-ink">{AGREEMENT_COPY.ownerOnlyBody}</p>
        </section>
      );
    }
    return (
      <p role="alert" data-testid="agreements-load-error" className="p-9 text-xs text-danger">
        {AGREEMENT_COPY.loadError}
      </p>
    );
  }

  // Guards the type only (RetrosPage.tsx:96-98): error is null and isLoading is
  // false here, so data is present.
  if (!agreements.data) {
    return null;
  }

  const doc = agreements.data;
  // Every write refuses while the household is locked (decision 22), so a
  // proposal, a history row or a live agreement PROVES this household once had
  // two owners. Sections alone do not: a section is a label, not a promise
  // (decision 8), and "Use starter set" is the one thing a locked household
  // could not have clicked either -- but a household that was unlocked, seeded
  // and then lost an owner has sections with nothing in them, which is the case
  // count > 0 keeps on the right side of this line.
  const hasContent =
    doc.proposals.length > 0 || doc.history.length > 0 || doc.sections.some((s) => s.count > 0);
  // The service's own per-section counts -- never `version - 1`, and never a
  // second total the wire would have to keep honest.
  const nothingAgreed = doc.sections.every((section) => section.count === 0);
  // GONE, not disabled (decision 3 and the spec's own wording). A disabled
  // button that cannot say why is the defect the admin flags screen already
  // carries.
  const canWrite = !doc.locked;
  // Version history stays in a locked household, being a read -- but only once
  // there is something to read. A household that never had two owners has an
  // empty history and no rows behind the button.
  const showHistory = !doc.locked || hasContent;

  return (
    <PageContainer data-testid="agreements-page">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <h1 className="text-[23px] font-semibold tracking-[-0.02em] text-ink">
            {AGREEMENT_COPY.title}
          </h1>
          {/* The version clause appends only once updatedAt is a real moment,
              never "v1, updated —". Task 11 renders the document below this
              header and leaves this element, and its testid, alone. */}
          <p data-testid="agreements-subtitle" className="mt-1 text-[13px] text-muted">
            {AGREEMENT_COPY.subtitle}
            {doc.updatedAt !== null &&
              ` · ${AGREEMENT_COPY.versionClause(doc.version, agreementDateLabel(doc.updatedAt))}`}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {/* Not a button -- nothing on it opens anything (the design's own
              static badge), so the 44px floor for interactive controls does not
              apply. RetrosPage.tsx:124-129's classes verbatim. */}
          <span
            data-testid="agreements-privacy-badge"
            className="inline-flex items-center rounded-lg border border-hairline bg-card px-3.5 py-2 text-[13px] text-muted"
          >
            {AGREEMENT_COPY.privacyBadge}
          </span>
          {canWrite && (
            <button
              type="button"
              data-testid="agreements-new-section"
              aria-haspopup="dialog"
              onClick={() => setNewSectionOpen(true)}
              className={HEADER_BUTTON}
            >
              {AGREEMENT_COPY.newSection}
            </button>
          )}
          {showHistory && (
            <button
              type="button"
              data-testid="agreements-history"
              aria-haspopup="dialog"
              onClick={() => setHistoryOpen(true)}
              className={HEADER_BUTTON}
            >
              {AGREEMENT_COPY.versionHistory}
            </button>
          )}
          {canWrite && (
            <button
              type="button"
              data-testid="agreements-propose"
              aria-haspopup="dialog"
              // "add" rather than a hardcoded "edit": the chips default from
              // the seed, and the header's own entry point is a new agreement.
              onClick={() => setProposeSeed({ mode: "add" })}
              className={CTA}
            >
              {AGREEMENT_COPY.proposeChange}
            </button>
          )}
        </div>
      </div>

      {doc.locked && !hasContent && (
        <section data-testid="agreements-locked-invite" className={PANEL}>
          <p aria-hidden="true" className="text-[28px]">
            {AGREEMENT_COPY.lockedTile}
          </p>
          <h2 className="mt-2 text-sm font-semibold text-ink">{AGREEMENT_COPY.lockedHeadline}</h2>
          <p className={MUTED}>{AGREEMENT_COPY.lockedBody}</p>
          <p className={MUTED}>{AGREEMENT_COPY.lockedOwnerCount}</p>
          {/* Into the existing Settings invite flow, not a second invite
              implementation (decision 2). settingsRoute's validateSearch, added
              in Step 7 below, is what makes this `search` prop typecheck at all
              -- which is why the route and this page land in one commit. */}
          <Link to="/settings" search={{ invite: true }} className={`mt-4 ${CTA}`}>
            {AGREEMENT_COPY.invitePartner}
          </Link>
        </section>
      )}

      {doc.locked && hasContent && (
        <section data-testid="agreements-frozen" className={PANEL}>
          <p className="text-[13px] text-ink">{AGREEMENT_COPY.frozenBanner}</p>
          {doc.proposals.length > 0 && (
            <p className={MUTED}>{AGREEMENT_COPY.frozenProposals(doc.proposals.length)}</p>
          )}
        </section>
      )}

      {!doc.locked && doc.sections.length === 0 && (
        <section data-testid="agreements-empty" className={PANEL}>
          <h2 className="text-sm font-semibold text-ink">{AGREEMENT_COPY.emptyHeadline}</h2>
          <p className={MUTED}>{AGREEMENT_COPY.emptyBody}</p>
          {/* "Add your first agreement" belongs beside this button and lands in
              Task 14 with the New-section modal it opens -- on this screen it
              must open New section, not Propose, because Propose's section
              select would be empty (the BillsPage dead end docs/LEARNING.md
              records) on the first screen anyone sees. Every control that opens
              a modal lands with that modal. */}
          <button
            type="button"
            data-testid="agreements-starter-set"
            onClick={handleStarterSet}
            disabled={seeding}
            className={`mt-4 ${CTA}`}
          >
            {AGREEMENT_COPY.useStarterSet}
          </button>
          {seedError && (
            <p role="alert" className="mt-2 text-xs text-danger">
              {seedError}
            </p>
          )}
          <h3 className="mt-5 text-xs text-muted">{AGREEMENT_COPY.popularHeading}</h3>
          {/* Read-only illustration: the design draws these four with hover
              styling and no onClick, and nothing says what tapping one would
              propose. <li>, not <button>, so nothing suggests otherwise. */}
          <ul className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-2">
            {AGREEMENT_COPY.popularCards.map((name) => (
              <li key={name} className={`${PANEL} text-[13px] text-ink`}>
                {name}
              </li>
            ))}
          </ul>
        </section>
      )}

      {!doc.locked && doc.sections.length > 0 && nothingAgreed && (
        <section data-testid="agreements-seeded" className={PANEL}>
          <h2 className="text-sm font-semibold text-ink">{AGREEMENT_COPY.seededHeadline}</h2>
          <p className={MUTED}>{AGREEMENT_COPY.seededBody(doc.sections.map((s) => s.name))}</p>
        </section>
      )}

      {/* Mount point. Task 12 renders doc.proposals as one block HERE, above
          the grid, for every state except the locked-invite one. Task 11 then
          renders the two-column sections grid below it, on the same condition
          -- a locked household with content still sees its document (decision
          3). Tasks 13, 14 and 15 mount their modals at the very end, each
          binding the state slot reserved for it at the top of this file. All
          four states above stay exactly as they are. */}
    </PageContainer>
  );
}
```

Two notes for whoever runs `make lint` between Steps 3 and 7. `search={{ invite: true }}` does **not** typecheck until `settingsRoute` gains its `validateSearch` in Step 7 — TanStack derives the allowed search shape from the route, and today's `settingsRoute` declares none. Vitest does not typecheck, so Step 4 passes regardless; `make lint` only goes green after Step 7. That is the concrete reason the route, the guard, the nav entry and this page are one commit, on top of the historical one (`110ab0a` split a route from its sidebar entry and shipped a link that 404'd).

- [ ] **Step 4: Run the page tests and watch them pass** — `cd web && npx vitest run src/features/marriage`. Expected: PASS — 7 in `AgreementsPage.test.tsx` and the 3 from Task 9, which now import their fixtures from the shared module.

- [ ] **Step 5: Write the failing wiring tests.**

In `web/src/features/shell/Sidebar.test.tsx`, rename the test at `:137` to `renders a Marriage group with Retros, Vision & goals and Agreements links for a member holding the capability` and add, beside its Vision assertions:

```tsx
    const agreementsLink = screen.getByRole("link", { name: "Agreements" });
    expect(agreementsLink).toHaveAttribute("href", "/marriage/agreements");
```

and to the without-capability test at `:154`:

```tsx
    expect(screen.queryByRole("link", { name: "Agreements" })).not.toBeInTheDocument();
```

In `web/src/routes/router.test.tsx`, beside the Vision route test at `:558`:

```tsx
  // The positive counterpart to the capability redirect: proves
  // marriageAgreementsRoute exists, sits under marriageGuardRoute, and mounts
  // the real page rather than a 404. The locked fixture is the cheapest valid
  // document -- one owner, nothing written -- and it exercises the <Link into
  // /settings?invite=true against the REAL route tree, which is the only place
  // settingsRoute's validateSearch is actually wired.
  it("mounts the Agreements page at /marriage/agreements for a caller who has the marriage capability", async () => {
    stubFetchRoutes({
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/marriage/agreements": {
        status: 200,
        body: {
          agreements: {
            locked: true,
            owners: [{ membershipId: "membership-1", name: "Andreas" }],
            version: 1,
            updatedAt: null,
            sections: [],
            proposals: [],
            history: [],
          },
        },
      },
    });

    const { router } = renderApp("/marriage/agreements");

    expect(await screen.findByTestId("agreements-page")).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "Our agreements" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/marriage/agreements");
  });

  // The invite deep link is this feature's own new work (decision 2), so it is
  // pinned in both directions: ?invite=true opens the modal, and anything else
  // fails CLOSED to {} rather than being carried around as an unvalidated
  // string. The three non-Members panels are stubbed as 500s deliberately --
  // their shapes are not this test's subject, each renders its own error line
  // rather than throwing (CurrencyPanel.tsx:124, NotificationsPanel.tsx:77,
  // SpacesPanel.tsx:39), and leaving a route unregistered would instead throw
  // inside stubFetchRoutes where TanStack Query swallows it into error state.
  it("/settings?invite=true lands with the invite modal open, and ?invite=maybe does not", async () => {
    const broke = { status: 500, body: { error: { code: "INTERNAL", message: "Broke." } } };
    const settingsStubs = {
      "GET /api/v1/auth/me": { status: 200, body: meFixture() },
      "GET /api/v1/household/members": { status: 200, body: [] },
      "GET /api/v1/household": broke,
      "GET /api/v1/spaces": broke,
      "GET /api/v1/notification-preferences": broke,
    };

    stubFetchRoutes(settingsStubs);
    const open = renderApp("/settings?invite=true");
    expect(await screen.findByRole("dialog")).toHaveTextContent("Invite a family member");
    expect(open.router.state.location.search).toEqual({ invite: true });
    open.unmount();

    stubFetchRoutes(settingsStubs);
    const closed = renderApp("/settings?invite=maybe");
    expect(
      await screen.findByRole("heading", { level: 1, name: "Settings" }),
    ).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(closed.router.state.location.search).toEqual({});
  });
```

And in `web/src/features/settings/MembersPanel.test.tsx` — the defect this design exists to prevent, which is that a **bound** `open` prop reopens the modal on the next render:

```tsx
  it("openInvite seeds the modal open, and closing it survives a re-render", async () => {
    stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture() },
      [`GET ${MEMBERS_URL}`]: { status: 200, body: [andreas, kayla, ethan] },
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    const panel = (
      <QueryClientProvider client={queryClient}>
        <MembersPanel openInvite />
      </QueryClientProvider>
    );

    const { rerender } = render(panel);

    fireEvent.click(await screen.findByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

    // The re-render is the whole test: a bound `open={openInvite}` would put
    // the dialog straight back for as long as the URL still carries
    // ?invite=true, so closing it would appear to do nothing the moment
    // anything else on Settings re-rendered.
    rerender(panel);
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
```

- [ ] **Step 6: Run them and watch them fail** — `cd web && npx vitest run src/routes src/features/shell src/features/settings/MembersPanel.test.tsx`.

Expected, and note that **none of these is a build failure**: vitest strips types with esbuild and never typechecks, so `<MembersPanel openInvite />` compiles fine today and the extra prop is simply ignored. Four **test** failures:

- Sidebar, `renders a Marriage group with…`: `Unable to find an accessible element with the role "link" and name "Agreements"`.
- `mounts the Agreements page at /marriage/agreements`: `findByTestId("agreements-page")` times out — no route matches, so `NotFoundScreen` renders instead.
- `/settings?invite=true lands with the invite modal open`: `findByRole("dialog")` times out. `location.search` is **not** the first assertion to fire, because `openInvite` is ignored and the modal never opens.
- `openInvite seeds the modal open`: `findByRole("button", { name: "Cancel" })` times out, for the same reason.

The TypeScript errors those four props and that `search` prop really are surface at Step 8's `make lint`, not here.

- [ ] **Step 7: Land the route, the nav entry, the guard and the deep link together.**

**(1)** `web/src/routes/router.tsx`, with the feature imports around `:97`, alphabetically before `RetrosPage`:

```tsx
import { AgreementsPage } from "../features/marriage/AgreementsPage";
```

**(2)** Straight after `marriageVisionRoute` ends at `:303`, and update the comment above `marriageRetrosRoute` at `:292-293`, whose "Agreements … will get its own sibling route when it's built" is now history:

```tsx
// Marriage's third and last page. A sibling of retros and vision under
// marriageGuardRoute, so RequireCapability("marriage") runs before it mounts;
// the server stacks requireOwner behind that, which is what the page's own 403
// branch answers.
const marriageAgreementsRoute = createRoute({
  getParentRoute: () => marriageGuardRoute,
  path: "agreements",
  component: AgreementsPage,
});
```

**(3)** Add `marriageAgreementsRoute` to the `marriageGuardRoute.addChildren([...])` list at `:667-671`, after `marriageVisionRoute`.

**(4)** Replace `settingsRoute` at `:319-326`:

```tsx
const settingsRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "settings",
  // ?invite=true opens the Members panel's invite modal on arrival -- the
  // Agreements page's locked state links here rather than growing a second
  // invite implementation (spec decision 2). TanStack's default search parser
  // JSON-parses each value, so a link built by <Link search={{ invite: true }}>
  // arrives as the boolean; a hand-typed or copied URL may still deliver the
  // string. Anything else -- ?invite=maybe, ?invite=1 -- fails CLOSED to {},
  // so an unvalidated value is never carried around in the URL.
  validateSearch: (search: Record<string, unknown>): { invite?: true } =>
    search.invite === true || search.invite === "true" ? { invite: true } : {},
  // Named, not an inline arrow, for the rules-of-hooks reason
  // adminHouseholdsRoute's own component gives at :476 -- it calls useSearch
  // directly, and eslint-plugin-react-hooks only recognises a function as a
  // component by its name starting with an uppercase letter. `from` is the
  // route ID, not the URL: authenticatedRoute and shellRoute are pathless but
  // still join the chain that identifies this route.
  component: function SettingsRouteComponent() {
    const { invite } = useSearch({ from: "/authenticated/shell/settings" });
    return <SettingsPage openInvite={invite === true} />;
  },
});
```

**(5)** `web/src/features/shell/Sidebar.tsx`, in `SPACE_PAGES.marriage` (`:51-54`), add the third entry and correct the comment's last sentence (`:49-50`) from "Task 11 added Vision & goals as the second -- Agreements is still ⬜." to "Task 11 added Vision & goals as the second, and the Agreements round added Agreements as the third and last.":

```tsx
    { label: "Agreements", to: "/marriage/agreements" },
```

**(6)** `web/src/features/settings/SettingsPage.tsx`, at `:12` and `:31`:

```tsx
// openInvite arrives from settingsRoute's validated ?invite=true and is passed
// straight through to the panel that owns the modal. Defaulted, so every
// existing `render(<SettingsPage />)` keeps compiling.
export function SettingsPage({ openInvite = false }: { openInvite?: boolean }) {
```

```tsx
        <MembersPanel openInvite={openInvite} />
```

**(7)** `web/src/features/settings/MembersPanel.tsx`, at `:229` and `:233`:

```tsx
export function MembersPanel({ openInvite = false }: { openInvite?: boolean }) {
```

```tsx
  // SEEDED, not bound. useState(openInvite) reads the prop once, on the first
  // render, and this panel owns the modal from then on. Binding it --
  // open={openInvite} on the modal at :335 -- would reopen it on every later
  // render for as long as the URL still carries ?invite=true, so closing it
  // would appear to do nothing the moment anything else on this page
  // re-rendered.
  const [inviteOpen, setInviteOpen] = useState(openInvite);
```

- [ ] **Step 8: Run everything and watch it pass** — `cd web && npx vitest run src/features/marriage src/features/shell src/features/settings src/routes`, then `cd .. && make lint`. Expected: PASS throughout, and lint green — this is the first point at which `search={{ invite: true }}` in `AgreementsPage.tsx` has a route schema to typecheck against.

- [ ] **Step 9: Three mutation checks, each pinning something different.**

  1. **The seed, not a binding.** In `MembersPanel.tsx`, restore `const [inviteOpen, setInviteOpen] = useState(false);` and change the modal at `:335` to `<InviteMemberModal open={openInvite || inviteOpen} … />`. Expected: **"openInvite seeds the modal open, and closing it survives a re-render"** goes red on the final assertion — `expected null not to be null` is *not* what you see; you see the dialog found again, `expected <dialog /> to not be in the document`. A **test** failure, and the other MembersPanel tests stay green because none of them passes `openInvite`. Restore.
  2. **The 403 branch.** In `AgreementsPage.tsx`, delete the `if (status === 403) { … }` block so both errors fall to the alert branch (the bug `BudgetPage.tsx` shipped). Expected: **"a limited member is told this is owner-only, not that something broke"** goes red on `findByTestId("agreements-owner-only")`, while **"a non-403 failure renders the alert…"** stays green — which is the point: only a test that distinguishes the two can catch this. A **test** failure. Restore.
  3. **Hidden, not merely absent by accident.** In `AgreementsPage.tsx`, change `const canWrite = !doc.locked;` to `const canWrite = true;`. Expected: **"a locked household with content keeps its document, names its frozen proposals, and keeps only Version history"** goes red on `queryByTestId("agreements-propose")` — `expected <button /> to not be in the document` — and **"a household that has never had two owners is offered the invite deep link and no write controls"** reddens on the same assertion. Both are **test** failures. Restore.

- [ ] **Step 10: Commit**

```bash
git add web/src/features/marriage/AgreementsPage.tsx web/src/features/marriage/AgreementsPage.test.tsx \
        web/src/features/marriage/agreementFixtures.tsx web/src/features/marriage/useAgreements.test.ts \
        web/src/routes/router.tsx web/src/routes/router.test.tsx \
        web/src/features/shell/Sidebar.tsx web/src/features/shell/Sidebar.test.tsx \
        web/src/features/settings/SettingsPage.tsx web/src/features/settings/MembersPanel.tsx \
        web/src/features/settings/MembersPanel.test.tsx
git commit -m "feat(agreements): four pre-document states, three header controls, the route and the invite deep link

Route, guard, sidebar entry and page in one change: 110ab0a shipped a link
that 404'd by splitting them, and here the page's <Link search={{ invite:
true }}> does not even typecheck until settingsRoute declares validateSearch.

Three mutation checks, all TEST failures (not build failures -- vitest strips
types with esbuild and never typechecks, so the wiring step's own run-and-fail
was four timeouts rather than a compile error):

1. Bound openInvite instead of seeding it (open={openInvite || inviteOpen}).
   The MembersPanel re-render test went red: the dialog came back after Cancel.
2. Deleted AgreementsPage's 403 branch so both errors took the alert path.
   The owner-only test went red while the 500 test stayed green.
3. Forced canWrite to true. Both locked-state tests went red on the absent
   'Propose a change' control -- gone, not disabled, is decision 3's wording."
```

---

### Task 11: The document, its sections and the derived numbering

**Files:**

- Create: `web/src/features/marriage/AgreementSectionCard.tsx`, `web/src/features/marriage/AgreementSectionCard.test.tsx`
- Modify: `web/src/features/marriage/AgreementsPage.tsx` (the answered-query branch only — the header, its subtitle and the four state panels are Task 10's and are not touched here), `web/src/features/marriage/AgreementsPage.test.tsx` (two new cases, on Task 10's helpers), `web/src/features/marriage/agreementCopy.ts` (one key appended to `AGREEMENT_COPY`)

**Interfaces:**

Consumes, all from Task 9 unless noted — copy these signatures, invent nothing:

```ts
// agreementSchemas.ts (Task 9)
export type AgreementSection = {
  id: string; name: string; count: number; visible: boolean;
  agreements: { id: string; number: number; body: string }[];
};
export type AgreementsDocument       // sections: AgreementSection[], and the rest of the wire

// agreementCopy.ts (Task 9)
export const AGREEMENT_COPY          // ONE object; this task appends one key to it

// AgreementsPage.test.tsx (Task 10) -- the shared frontend test helpers
const DOC_URL = "/api/v1/marriage/agreements";
function renderPage(routes: Record<string, RouteResponse | RouteResponse[]>): { stub: ReturnType<typeof stubFetchRoutes> } & ReturnType<typeof renderWithRouter>;
// renderPage registers `GET /api/v1/auth/me` itself, merges the caller's routes
// over that map, calls stubFetchRoutes and renders <AgreementsPage /> through
// renderWithRouter. The page calls useMe(), and stubFetchRoutes throws on an
// unregistered request, so no page test may leave /auth/me out.
function documentFixture(o?: Partial<AgreementsDocument>): AgreementsDocument;
const ONE_OWNER: { membershipId: string; name: string }[];   // one owner, "Andreas"
```

Produces:

```tsx
// AgreementSectionCard.tsx
export function AgreementSectionCard({ section }: { section: AgreementSection }): JSX.Element
// agreementCopy.ts, appended INSIDE the existing AGREEMENT_COPY object literal
sectionCount: (n: number) => string          // "1 agreement" / "3 agreements"
// AgreementsPage.test.tsx, beside Task 10's helpers, for Tasks 12-15 to reuse
function sectionFixture(name: string, first: number, bodies: string[]): AgreementSection;
```

`AgreementSection` is **imported**, never re-declared: Task 9 already exports that name off the one document type, and a second `type AgreementSection` in this file is two names for one wire shape that can drift apart. There is no `AGREEMENT_DOCUMENT_COPY` and no `SECTION_CARD_COPY` either — every string in this feature lives in `AGREEMENT_COPY`, and the subtitle and its version clause (`AGREEMENT_COPY.subtitle`, `AGREEMENT_COPY.versionClause`) already landed in Tasks 9 and 10. **Leave the `<p data-testid="agreements-subtitle">` exactly as Task 10 wrote it**: its locked-with-content case asserts on that test id, and replacing the element is how this task would turn a green suite red for no reason.

- [ ] **Step 1: Write the failing tests**

First `web/src/features/marriage/AgreementSectionCard.test.tsx` — props in, no router and no network, `PillarCard.test.tsx`'s shape for a pure-presentation component:

```tsx
// Props in, nothing else: no router, no network -- PillarCard.test.tsx's shape.
// The fixture is local because the shared ones live in AgreementsPage.test.tsx,
// and importing from another test file would re-run that file's suite.
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { AgreementSectionCard } from "./AgreementSectionCard";
import type { AgreementSection } from "./agreementSchemas";

function sectionFixture(name: string, first: number, bodies: string[]): AgreementSection {
  return {
    id: `sec-${name}`,
    name,
    count: bodies.length,
    visible: bodies.length > 0,
    agreements: bodies.map((body, i) => ({ id: `${name}-${i}`, number: first + i, body })),
  };
}

describe("AgreementSectionCard", () => {
  it("renders the section name, its count and every row in the order the server sent", () => {
    render(
      <AgreementSectionCard
        section={sectionFixture("Money", 3, [
          "Any purchase over S$500 gets discussed first.",
          "We review the budget on the first Sunday.",
          "Neither of us lends money without telling the other.",
        ])}
      />,
    );

    expect(screen.getByRole("heading", { level: 3, name: "Money" })).toBeInTheDocument();
    expect(screen.getByText("3 agreements")).toBeInTheDocument();
    const rows = screen.getAllByTestId(/^agreement-row-/);
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("03");
    expect(rows[0]).toHaveTextContent("Any purchase over S$500 gets discussed first.");
    expect(rows[2]).toHaveTextContent("05");
  });

  // The service composes the integer for the whole document on every read
  // (decisions 10 and 11) and the browser owns the padding, so this pins the
  // padding and nothing else: a card that derived its own numbers would
  // restart every section at 01.
  it("pads a single-digit number to two digits and leaves a two-digit one alone", () => {
    render(<AgreementSectionCard section={sectionFixture("Us", 9, ["A weekly walk", "One night out a month"])} />);

    expect(within(screen.getByTestId("agreement-row-Us-0")).getByText("09")).toBeInTheDocument();
    expect(within(screen.getByTestId("agreement-row-Us-1")).getByText("10")).toBeInTheDocument();
  });

  it("says 1 agreement, not 1 agreements", () => {
    render(<AgreementSectionCard section={sectionFixture("Conflict", 1, ["No raised voices in front of the kids."])} />);

    expect(screen.getByText("1 agreement")).toBeInTheDocument();
    expect(screen.queryByText("1 agreements")).not.toBeInTheDocument();
  });
});
```

Then two cases in `web/src/features/marriage/AgreementsPage.test.tsx`, on Task 10's `renderPage`, `documentFixture` and `ONE_OWNER`. Add `within` to that file's `@testing-library/react` import and `AgreementSection` to its `import type { … } from "./agreementSchemas"` line, then put `sectionFixture` and `headingsIn` beside Task 10's helpers — Tasks 12–15 reuse `sectionFixture` by that name:

```tsx
function sectionFixture(name: string, first: number, bodies: string[]): AgreementSection {
  return {
    id: `sec-${name}`,
    name,
    count: bodies.length,
    visible: bodies.length > 0,
    agreements: bodies.map((body, i) => ({ id: `${name}-${i}`, number: first + i, body })),
  };
}
const headingsIn = (testId: string) =>
  within(screen.getByTestId(testId))
    .getAllByRole("heading", { level: 3 })
    .map((heading) => heading.textContent);

  // Column-MAJOR: the design's 01-12 runs continuously down one column and on
  // into the next, so Money 01-02 and Conflict 03-05 are the left column. The
  // row-major fill a grid-cols-2 produces would put Conflict beside Money and
  // zig-zag the numbering down the page. `visible` is the server's own flag
  // (decision 8), never a rule re-derived here.
  it("renders the visible sections in two column-major columns, skipping the ones the server hid", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 200,
        body: {
          agreements: documentFixture({
            sections: [
              sectionFixture("Money", 1, ["One shared account", "Any purchase over S$500 gets discussed first."]),
              sectionFixture("Conflict", 3, ["No raised voices", "No silent treatment", "We finish it the same day"]),
              sectionFixture("Home & kids", 6, ["Bedtime is a two-person job", "Saturday mornings are the kids'"]),
              sectionFixture("Us", 8, ["One night out a month"]),
              sectionFixture("Faith & values", 9, []),
            ],
          }),
        },
      },
    });

    await screen.findByTestId("agreements-column-0");
    expect(headingsIn("agreements-column-0")).toEqual(["Money", "Conflict"]);
    expect(headingsIn("agreements-column-1")).toEqual(["Home & kids", "Us"]);
    // An empty section stays invisible in the document (decision 8) while the
    // propose picker still offers it, which Task 13 reads off the same array.
    expect(screen.queryByText("Faith & values")).not.toBeInTheDocument();
    expect(screen.getByText("03")).toBeInTheDocument();
    expect(screen.getByText("3 agreements")).toBeInTheDocument();
  });

  // Decision 3: two owners can become one, and the promise those two people
  // made does not stop existing when one of them does. The document renders;
  // the banner explains; nothing here is a write.
  it("a locked household still sees its whole document under the frozen banner", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 200,
        body: {
          agreements: documentFixture({
            locked: true,
            owners: ONE_OWNER,
            version: 2,
            updatedAt: "2026-06-28T21:18:52+08:00",
            sections: [sectionFixture("Money", 1, ["One shared account", "Any purchase over S$500 gets discussed first."])],
          }),
        },
      },
    });

    expect(await screen.findByTestId("agreements-frozen")).toBeInTheDocument();
    expect(headingsIn("agreements-column-0")).toEqual(["Money"]);
    expect(screen.getByTestId("agreement-row-Money-0")).toHaveTextContent("One shared account");
  });
```

- [ ] **Step 2: Run them and watch them fail**

```bash
cd web && npx vitest run src/features/marriage/AgreementSectionCard.test.tsx
```

Expected: FAIL — `Error: Failed to resolve import "./AgreementSectionCard" from "src/features/marriage/AgreementSectionCard.test.tsx". Does the file exist?` That is a **build** failure, not a test failure; record which you saw in the commit body.

```bash
cd web && npx vitest run src/features/marriage/AgreementsPage.test.tsx
```

Expected: FAIL — both new cases report `TestingLibraryElementError: Unable to find an element by: [data-testid="agreements-column-0"]`. That is a **test** failure: Task 10's page renders its four states and no document. Every case Task 10 wrote stays green.

- [ ] **Step 3: Append the count string to `AGREEMENT_COPY`**

One key, inside the object literal Task 9 created — not a second export. Check before typing: a duplicate key in one object literal is a TypeScript error.

```ts
  // "3 agreements" under the section name. The count is the service's own
  // (agreementSectionDTO.Count), never section.agreements.length: a total and
  // its breakdown that apply different filters quietly stop reconciling, which
  // browser criterion 7 exists to catch.
  sectionCount: (n: number) => (n === 1 ? "1 agreement" : `${n} agreements`),
```

- [ ] **Step 4: Write the card**

Create `web/src/features/marriage/AgreementSectionCard.tsx`. `PillarCard.tsx:48-85` is the panel and the header rhythm verbatim, so the two marriage documents read as one screen family:

```tsx
// One section of the agreements document: its name, its live count and its
// numbered rows. A pure presentation component -- it takes the section the
// page already fetched, so nothing here reaches the network.
import { AGREEMENT_COPY } from "./agreementCopy";
import type { AgreementSection } from "./agreementSchemas";

export function AgreementSectionCard({ section }: { section: AgreementSection }) {
  return (
    <div
      data-testid={`agreement-section-${section.id}`}
      className="rounded-xl border border-hairline bg-card p-[22px]"
    >
      <div className="mb-3.5 flex items-baseline justify-between gap-3">
        {/* h3 under the page's h1: those are the only two heading levels on
            this screen, so a screen reader's outline matches what is drawn. */}
        <h3 className="text-sm font-semibold text-ink">{section.name}</h3>
        {/* shrink-0 + whitespace-nowrap for PillarCard's own reason: a long
            section name crowds this span for width, and without them the
            count wraps mid-phrase ("3" / "agreements"). */}
        <span className="shrink-0 whitespace-nowrap text-[11px] text-muted">
          {AGREEMENT_COPY.sectionCount(section.count)}
        </span>
      </div>
      <div className="flex flex-col gap-3 text-[13px] leading-[1.55] text-label">
        {section.agreements.map((agreement) => (
          <div key={agreement.id} data-testid={`agreement-row-${agreement.id}`} className="flex gap-2.5">
            {/* The service composes this integer for the whole document on
                every read and stores it nowhere (decisions 10 and 11), so this
                card pads and derives nothing -- which is why a section
                legitimately starts at 03. Deriving a number here would restart
                every section at 01 and break the design's continuous run. */}
            <span className="flex-none font-semibold text-accent">
              {String(agreement.number).padStart(2, "0")}
            </span>
            <span>{agreement.body}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 5: Write the document half of the page**

In `AgreementsPage.tsx`, add the import beside Task 10's feature imports:

```tsx
import { AgreementSectionCard } from "./AgreementSectionCard";
```

Then, in the branch that renders from an **answered** query — after Task 10's `hasContent` and `nothingAgreed` constants, before the `return`:

```tsx
  // `visible` is the server's own flag (decision 8), never a rule re-derived
  // here; the propose picker (Task 13) is handed doc.sections whole, empty
  // sections included, off this same array.
  const visible = doc.sections.filter((section) => section.visible);
  // Column-MAJOR, and deliberately not `grid-cols-2`: a row-major fill would
  // put Conflict's 03-05 beside Money's 01-02 and zig-zag the design's
  // continuous numbering down the page. Below `lg` the two wrappers stack, so
  // server order (created_at, id -- decision 11) holds at every width.
  const half = Math.ceil(visible.length / 2);
  const columns = [visible.slice(0, half), visible.slice(half)];
```

and inside the `<PageContainer data-testid="agreements-page">`, **after** the four state panels Task 10 wrote and after the proposal block Task 12 mounts, as the last child:

```tsx
      {visible.length > 0 && (
        <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-2">
          {columns.map((column, i) => (
            <div key={i} data-testid={`agreements-column-${i}`} className="flex flex-col gap-4">
              {column.map((section) => (
                <AgreementSectionCard key={section.id} section={section} />
              ))}
            </div>
          ))}
        </div>
      )}
```

The `visible.length > 0` gate keeps two empty column wrappers off the three states that have nothing to show. Nothing else in the file changes: the header, the subtitle and its `data-testid="agreements-subtitle"`, the four panels and the starter-set handler are Task 10's and stay exactly as they are.

- [ ] **Step 6: Run the tests and watch them pass**

```bash
cd web && npx vitest run src/features/marriage
```

Expected: PASS — the 3 cases in `AgreementSectionCard.test.tsx`, the 2 new cases in `AgreementsPage.test.tsx`, and every earlier case in that file and in the rest of `src/features/marriage` still green.

- [ ] **Step 7: Mutation-check the split and the padding**

**The split.** Replace the two slices with the row-major fill a `grid-cols-2` produces:

```tsx
  const columns = [visible.filter((_, i) => i % 2 === 0), visible.filter((_, i) => i % 2 === 1)];
```

Expected: **test** failure. `renders the visible sections in two column-major columns, skipping the ones the server hid` goes red on the first `toEqual`, expected `["Money", "Conflict"]`, received `["Money", "Home & kids"]` — an ordering assertion, not a missing element, which is what proves the test is about the fill and not about the sections existing. Restore.

**The padding.** Delete `.padStart(2, "0")` from `AgreementSectionCard.tsx`, leaving `{String(agreement.number)}`. Expected: **test** failure. `pads a single-digit number to two digits and leaves a two-digit one alone` goes red with `Unable to find an element with the text: 09`, and the document case goes red on `03` alongside it — the two-digit half of the same test stays green, which is what shows the mutation hit the padding and not the numbering. Restore.

- [ ] **Step 8: Commit**

```bash
cd web && npx vitest run && cd .. && make lint
git add web/src/features/marriage/AgreementSectionCard.tsx \
        web/src/features/marriage/AgreementSectionCard.test.tsx \
        web/src/features/marriage/AgreementsPage.tsx \
        web/src/features/marriage/AgreementsPage.test.tsx \
        web/src/features/marriage/agreementCopy.ts
git commit -m "feat(agreements): the document, its sections, and numbering that stays continuous

Two mutation checks, both test failures rather than build failures:

- Replaced the two column-major slices with the row-major fill grid-cols-2
  produces. 'renders the visible sections in two column-major columns' went
  red on the column-0 headings: expected [Money, Conflict], received
  [Money, Home & kids]. An ordering assertion, not a missing element.
- Deleted .padStart(2, 0) from AgreementSectionCard. 'pads a single-digit
  number to two digits' went red with 'Unable to find an element with the
  text: 09', and the document case went red on 03 with it, while the
  two-digit half stayed green.

The build failure this task also saw was the expected one at Step 2:
Failed to resolve import ./AgreementSectionCard, before the file existed."
```

---

### Task 12: `ProposalCard`, and the block the page mounts it in

**Files:**

- Create: `web/src/features/marriage/ProposalCard.tsx`, `web/src/features/marriage/ProposalCard.test.tsx`
- Modify: `web/src/features/marriage/agreementCopy.ts` (fifteen keys appended to `AGREEMENT_COPY`, plus one exported composer), `web/src/features/marriage/AgreementsPage.tsx` (the proposal block, above Task 11's grid), `web/src/features/marriage/AgreementsPage.test.tsx` (three new cases)

**Interfaces:**

Consumes — copy these signatures, invent nothing:

```ts
// agreementSchemas.ts (Task 9). status is FOUR-valued on the wire: a write
// response carries the row at the status it now holds. Narrowing to
// pending/parked is this card's own switch, never the schema's job.
export type AgreementProposal = {
  id: string; kind: "add" | "edit" | "remove";
  status: "pending" | "parked" | "accepted" | "withdrawn";
  sectionId: string; sectionName: string; targetAgreementId: string;
  body: string; previousBody: string; note: string; parkNote: string;
  proposedByMembershipId: string; proposedByName: string; proposedAt: string;
  awaitingNames: string[]; targetChanged: boolean;
  canAgree: boolean; canWithdraw: boolean;
};

// agreementCopy.ts (Task 9)
export const AGREEMENT_COPY                                  // ONE object; this task appends to it
export function joinNames(names: string[]): string           // "Christine and Ibu" -- "and", not "&"
export function agreementDateLabel(iso: string): string      // "14 Jul", no year

// useAgreements.ts (Task 9)
export function handleWriteError(err: unknown, reload: () => Promise<void>, fallback: string): string
export function useAgreements(): {
  data: AgreementsDocument | undefined
  reload: () => Promise<void>
  agree: (proposalId: string) => Promise<AgreementsDocument>
  park: (proposalId: string, note: string) => Promise<AgreementsDocument>
  withdraw: (proposalId: string) => Promise<AgreementsDocument>
  // ...and the rest of the hook's contract, which this task does not use
}

// AgreementsPage.test.tsx -- shared test helpers (Task 10, plus Task 11's sectionFixture)
const DOC_URL = "/api/v1/marriage/agreements";
function renderPage(routes: Record<string, RouteResponse | RouteResponse[]>): { stub: ReturnType<typeof stubFetchRoutes> } & ReturnType<typeof renderWithRouter>;
// renderPage registers `GET /api/v1/auth/me` itself, merges the caller's routes
// over that map, calls stubFetchRoutes and renders <AgreementsPage /> through
// renderWithRouter. The page calls useMe() and stubFetchRoutes throws on an
// unregistered request, so no page test may leave /auth/me out.
function documentFixture(o?: Partial<AgreementsDocument>): AgreementsDocument;
function proposalFixture(o?: Partial<AgreementProposal>): AgreementProposal;
function sectionFixture(name: string, first: number, bodies: string[]): AgreementSection;
const ONE_OWNER: { membershipId: string; name: string }[];
```

Produces:

```tsx
// ProposalCard.tsx
export type ProposalCardProps = {
  proposal: AgreementProposal;
  targetNumber: number | null;
  onAgree: (proposalId: string) => Promise<string | null>;
  onPark: (proposalId: string, note: string) => Promise<string | null>;
  onWithdraw: (proposalId: string) => Promise<string | null>;
};
export function ProposalCard(props: ProposalCardProps): JSX.Element | null

// agreementCopy.ts
export function proposalSummary(
  kind: AgreementProposal["kind"], proposedByName: string, sectionName: string, targetNumber: number | null,
): string
// ...plus fifteen keys inside AGREEMENT_COPY (Step 3 lists every value)
```

Three things about those props:

- **The handlers resolve to `null` when the write landed, or to the sentence to show.** The page has already run the failure through `handleWriteError(err, reload, fallback)`, which owns the refetch that turns a `409 AGREEMENT_CHANGED` into a `targetChanged` card. **No `hadConflict` latch here, deliberately**: this card holds no draft to lose, so the refetch *is* the fix (the latch belongs to `ProposeAgreementModal`, Task 13).
- **`targetNumber` is the target's display number as the document numbers it right now**, `null` when no live agreement carries that id — the `targetChanged` case. The wire carries no number on a proposal: numbering is the document's, derived at render (decision 11).
- **Mutations arrive as props, never a `useAgreements()` call per card** — a hook per card is several callers racing one cache entry.

`AGREEMENT_COPY.agree` lands here, in the one copy object, because Task 16's To-discuss block renders that same string. Task 16 owns a **different** composer, `parkedSummary`, for its one-line rows; the two do not merge, and neither name collides.

- [ ] **Step 1: Write the failing card test**

Create `web/src/features/marriage/ProposalCard.test.tsx`. Props in, no router and no network. **`fireEvent`, never `userEvent`** — `@testing-library/user-event` is not in `web/package.json` and Global Constraints forbid new dependencies; `fireEvent.change` is what replaces `userEvent.type`. The matrix is the point of the file: every button is decided by a flag the server stamped, never by a membership id compared in the browser.

```tsx
// Props in, nothing else: no router, no network -- PillarCard.test.tsx's shape.
// fireEvent, never userEvent: @testing-library/user-event is not a dependency
// of this project and Global Constraints forbid adding one.
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { ProposalCard } from "./ProposalCard";
import type { AgreementProposal } from "./agreementSchemas";

// The window.confirm spy in the withdraw case would otherwise leak into every
// test that runs after it.
afterEach(() => {
  vi.restoreAllMocks();
});

// Local, because the shared fixtures live in AgreementsPage.test.tsx and
// importing from another test file would re-run that file's suite. Same
// defaults as Task 9's, so a reader comparing the two is comparing like
// with like.
function proposalFixture(o: Partial<AgreementProposal> = {}): AgreementProposal {
  return {
    id: "p-1",
    kind: "add",
    status: "pending",
    sectionId: "s-1",
    sectionName: "Money",
    targetAgreementId: "",
    body: "Any purchase over S$500 gets discussed first.",
    previousBody: "",
    note: "",
    parkNote: "",
    proposedByMembershipId: "m-1",
    proposedByName: "Andreas",
    proposedAt: "2026-07-14T09:00:00+08:00",
    awaitingNames: ["Christine"],
    targetChanged: false,
    canAgree: true,
    canWithdraw: false,
    ...o,
  };
}

function renderCard(o: Partial<AgreementProposal> = {}, targetNumber: number | null = null) {
  const onAgree = vi.fn<(id: string) => Promise<string | null>>().mockResolvedValue(null);
  const onPark = vi.fn<(id: string, note: string) => Promise<string | null>>().mockResolvedValue(null);
  const onWithdraw = vi.fn<(id: string) => Promise<string | null>>().mockResolvedValue(null);
  const view = render(
    <ProposalCard
      proposal={proposalFixture(o)}
      targetNumber={targetNumber}
      onAgree={onAgree}
      onPark={onPark}
      onWithdraw={onWithdraw}
    />,
  );
  return { ...view, onAgree, onPark, onWithdraw };
}

// [case, overrides, buttons that must be there, buttons that must not]
const matrix: [string, Partial<AgreementProposal>, string[], string[]][] = [
  ["the proposer, waiting on the other owner", { canAgree: false, canWithdraw: true }, ["Withdraw"], ["Agree", "Discuss"]],
  ["the other owner on a pending change", { canAgree: true, canWithdraw: false }, ["Agree", "Discuss"], ["Withdraw"]],
  ["the other owner on a parked change", { status: "parked", canAgree: true, canWithdraw: false }, ["Agree"], ["Discuss", "Withdraw"]],
  ["everyone still here has already agreed", { canAgree: true, canWithdraw: true, awaitingNames: [] }, ["Agree", "Withdraw"], ["Discuss"]],
  // Decision 16's trap, and the reason canAgree alone decides Agree: canAgree
  // is `!locked && ...`, so a locked household reaches this row with an empty
  // awaiting list and must be offered nothing. A test on
  // `awaitingNames.length === 0` alone would put an Agree button on a page
  // whose every write refuses with 409.
  ["a locked household whose awaiting list emptied", { canAgree: false, canWithdraw: false, awaitingNames: [] }, [], ["Agree", "Discuss", "Withdraw"]],
];

describe("ProposalCard", () => {
  it.each(matrix)("offers the right actions to %s", (_case, overrides, shown, hidden) => {
    renderCard(overrides);

    shown.forEach((label) => expect(screen.getByRole("button", { name: label })).toBeInTheDocument());
    hidden.forEach((label) => expect(screen.queryByRole("button", { name: label })).not.toBeInTheDocument());
  });

  // doc.proposals excludes accepted and withdrawn in SQL, so a fourth status
  // here is a row nothing wrote. The card refuses rather than guessing a title
  // for it -- a write response may legitimately carry one, and no screen
  // renders that field.
  it("renders nothing at all for a status the document never carries", () => {
    expect(renderCard({ status: "accepted" }).container).toBeEmptyDOMElement();
  });

  it("titles a pending change with everyone it waits for, and an edit shows both wordings", () => {
    renderCard(
      {
        kind: "edit",
        targetAgreementId: "a-2",
        previousBody: "Each gets S$200/mo. no questions asked.",
        body: "Each gets S$250/mo. no questions asked.",
        awaitingNames: ["Christine", "Ibu"],
      },
      2,
    );

    // joinNames joins with "and", not the design's "&" -- one function, three
    // call sites, so three owners cannot read correctly here and wrongly in
    // version history. The dash is an em dash (U+2014).
    expect(screen.getByText("Pending change — needs Christine and Ibu")).toBeInTheDocument();
    expect(screen.getByText(/Andreas proposed changing Money 02:/)).toBeInTheDocument();
    expect(screen.getByTestId("proposal-previous-body")).toHaveClass("line-through");
    expect(screen.getByTestId("proposal-body")).toHaveTextContent("S$250");
  });

  // Decision 16 again, from the other side: the sentence that explains why
  // Agree is still offered when nobody is being waited on.
  it("explains why Agree is still offered once the awaiting list has emptied", () => {
    renderCard({ canAgree: true, awaitingNames: [] });

    expect(screen.getByText("Pending change")).toBeInTheDocument();
    expect(screen.getByTestId("proposal-everyone-agreed")).toHaveTextContent(
      "Everyone still here has agreed — Agree once more to make it final.",
    );
  });

  it("shows a park note under the To discuss label, and nothing when the note is empty", () => {
    const { rerender, onAgree, onPark, onWithdraw } = renderCard({ status: "parked", parkNote: "The ceiling feels low" });

    expect(screen.getByTestId("proposal-park-note")).toHaveTextContent("To discuss The ceiling feels low");
    // An empty park note is ordinary, not a defect: Discuss is a bare button
    // and decision 7 stores whatever was typed, including nothing.
    rerender(
      <ProposalCard
        proposal={proposalFixture({ status: "parked", parkNote: "" })}
        targetNumber={null}
        onAgree={onAgree}
        onPark={onPark}
        onWithdraw={onWithdraw}
      />,
    );
    expect(screen.queryByTestId("proposal-park-note")).not.toBeInTheDocument();
  });

  // Decision 14: the sentence depends on who is reading, never on who
  // proposed it -- canWithdraw already carries decision 15's fallback, so a
  // household whose proposer has left is never told to ask a ghost.
  it.each([
    [{ canWithdraw: true }, "Withdraw it and propose the change again."],
    [{ canWithdraw: false, proposedByName: "Andreas" }, "Ask Andreas to withdraw it and propose it again against the current wording."],
    [{ canWithdraw: false, proposedByName: "" }, "This needs withdrawing and proposing again against the current wording."],
  ] as [Partial<AgreementProposal>, string][])("addresses a stale proposal to whoever is reading (%#)", (overrides, sentence) => {
    renderCard({ targetChanged: true, canAgree: true, ...overrides });

    expect(screen.getByTestId("proposal-stale-note")).toHaveTextContent(sentence);
    expect(screen.getByRole("button", { name: "Agree" })).toBeDisabled();
  });

  it("expands Discuss inside the card and parks with the note typed there", async () => {
    const { onPark } = renderCard({ canAgree: true, canWithdraw: false });

    fireEvent.click(screen.getByRole("button", { name: "Discuss" }));
    fireEvent.change(screen.getByLabelText("What you want to talk through (optional)"), {
      target: { value: "The ceiling feels low" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Park for next retro" }));

    await waitFor(() => expect(onPark).toHaveBeenCalledWith("p-1", "The ceiling feels low"));
    // The panel closes on success, so the note cannot be sent twice.
    await waitFor(() => expect(screen.queryByTestId("proposal-discuss")).not.toBeInTheDocument());
  });

  it("confirms a withdrawal in the card, never through window.confirm, and shows a failure in place", async () => {
    const confirmSpy = vi.spyOn(window, "confirm");
    const { onWithdraw } = renderCard({ canWithdraw: true });
    onWithdraw.mockResolvedValue("Someone changed this while you were reading.");

    fireEvent.click(screen.getByRole("button", { name: "Withdraw" }));

    expect(onWithdraw).not.toHaveBeenCalled();
    expect(confirmSpy).not.toHaveBeenCalled();
    expect(screen.getByTestId("proposal-withdraw-confirm")).toHaveTextContent(
      "Withdraw this proposal? It stops waiting for anyone, and nothing in the document changes.",
    );

    fireEvent.click(screen.getByRole("button", { name: "Withdraw it" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Someone changed this while you were reading.");
    expect(onWithdraw).toHaveBeenCalledWith("p-1");
  });
});
```

- [ ] **Step 2: Run it and watch it fail**

```bash
cd web && npx vitest run src/features/marriage/ProposalCard.test.tsx
```

Expected: FAIL — `Error: Failed to resolve import "./ProposalCard" from "src/features/marriage/ProposalCard.test.tsx". Does the file exist?` That is a **build** failure, not a test failure; record which you saw in the commit body.

- [ ] **Step 3: Append the copy and the kind switch**

Eighteen keys **inside the existing `AGREEMENT_COPY` object literal** — one copy module, one object; a duplicate key is a TypeScript error, so read what Task 9 left there before typing. `cancel` is introduced here and Tasks 13-15 reuse it rather than declaring their own. `cancel` is introduced here and Tasks 13–15 reuse it rather than declaring their own.

```ts
  // The awaiting clause is dropped rather than left dangling: decision 16's
  // household has an empty list, and "Pending change — needs " is not a
  // sentence. joinNames generalises the design's literal "needs Christine" to
  // any number of owners (decision 4). Both dashes are em dashes (U+2014).
  pendingTitle: (awaiting: string[]) =>
    awaiting.length === 0 ? "Pending change" : `Pending change — needs ${joinNames(awaiting)}`,
  parkedTitle: (awaiting: string[]) =>
    awaiting.length === 0 ? "Parked for the next retro" : `Parked for the next retro — needs ${joinNames(awaiting)}`,
  everyoneAgreed: "Everyone still here has agreed — Agree once more to make it final.",
  toDiscuss: "To discuss",
  // Decision 14's three sentences. Which one shows is decided by canWithdraw,
  // never by "did I propose this": canWithdraw already carries decision 15's
  // fallback, so a household whose proposer has left is not told to ask a
  // ghost. staleNoName is that household's own case -- proposedByName is ""
  // when the membership no longer resolves (decision 20).
  staleMine:
    "This no longer matches the agreement it was written against, so it can't be agreed. Withdraw it and propose the change again.",
  staleTheirs: (proposer: string) =>
    `Ask ${proposer} to withdraw it and propose it again against the current wording.`,
  staleNoName: "This needs withdrawing and proposing again against the current wording.",
  agree: "Agree",
  discuss: "Discuss",
  withdraw: "Withdraw",
  // Introduced here and reused by Tasks 13-15 rather than redeclared: one
  // object, and a duplicate key is a TypeScript error.
  cancel: "Cancel",
  parkNoteLabel: "What you want to talk through (optional)",
  parkAction: "Park for next retro",
  withdrawConfirmBody:
    "Withdraw this proposal? It stops waiting for anyone, and nothing in the document changes.",
  withdrawConfirmAction: "Withdraw it",
  // The three fallbacks the page passes to handleWriteError -- shown only when
  // the failure is a genuine server failure rather than one of the refusals
  // the hook names.
  agreeError: "Couldn't agree that just now.",
  parkError: "Couldn't park that for the retro just now.",
  withdrawError: "Couldn't withdraw that just now.",
```

Then the summary composer, in the same file, beside `joinNames`. It needs the proposal's `kind` type, so `agreementCopy.ts` imports it as a type the way `goalCopy.ts` imports `GoalContribution`:

```ts
import type { AgreementProposal } from "./agreementSchemas";

// The card's one-line summary, composed from the fields that exist because
// there is no summary column on the wire. The target reads "Money 02" when the
// document still numbers it and just "Money" when it does not (targetChanged),
// since the number is the document's and derived at render (decision 11).
//
// Attribution falls back to "Proposed ..." when proposedByName is "" -- the
// membership no longer resolves, which decision 20 makes ordinary for any
// household a partner has left, not a corruption.
//
// The default is fail-closed with no guess: agreementProposalSchema's z.enum
// already refused any other kind one layer up, and `const refused: never` is
// the compile-time proof that this switch covers the enum. It returns "" for
// that unreachable row rather than inventing a user-visible sentence -- and
// therefore invents no copy key either.
export function proposalSummary(
  kind: AgreementProposal["kind"],
  proposedByName: string,
  sectionName: string,
  targetNumber: number | null,
): string {
  const target = targetNumber === null ? sectionName : `${sectionName} ${String(targetNumber).padStart(2, "0")}`;
  const opening = (verb: string) => (proposedByName === "" ? `Proposed: ${verb}` : `${proposedByName} proposed ${verb}`);
  switch (kind) {
    case "add":
      return `${opening("adding to")} ${sectionName}:`;
    case "edit":
      return `${opening("changing")} ${target}:`;
    case "remove":
      return `${opening("removing")} ${target}:`;
    default: {
      const refused: never = kind;
      void refused;
      return "";
    }
  }
}
```

- [ ] **Step 4: Write the card**

Create `web/src/features/marriage/ProposalCard.tsx`, in full:

```tsx
// One open proposal -- pending or parked -- and the actions the server says
// this viewer may take. Mutations arrive as props, never a useAgreements()
// call per card: a hook per card is several callers racing one cache entry.
import { useState } from "react";
import { AGREEMENT_COPY, agreementDateLabel, proposalSummary } from "./agreementCopy";
import type { AgreementProposal } from "./agreementSchemas";

export type ProposalCardProps = {
  proposal: AgreementProposal;
  // The target's display number as the document numbers it right now, and null
  // when no live agreement carries that id -- the targetChanged case. The wire
  // carries no number on a proposal (decision 11).
  targetNumber: number | null;
  // Each resolves to null when the write landed, or to the sentence to show.
  // The page has already run the failure through handleWriteError, which owns
  // the refetch that turns a 409 AGREEMENT_CHANGED into a targetChanged card.
  onAgree: (proposalId: string) => Promise<string | null>;
  onPark: (proposalId: string, note: string) => Promise<string | null>;
  onWithdraw: (proposalId: string) => Promise<string | null>;
};

type Action = "agree" | "park" | "withdraw";

// Six buttons and every text line share these, so a change lands once. Each is
// a COMPLETE string on purpose: two Tailwind utilities for the same property
// on one element resolve by stylesheet order rather than by the order they are
// written, so `${primary} bg-danger` is a coin toss between accent and danger.
// RetroModal.tsx:640-648 spells its own danger button out for the same reason.
const primary =
  "min-h-11 rounded-lg bg-accent px-3.5 py-2 text-xs font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0";
const secondary =
  "min-h-11 rounded-lg border border-callout-border bg-card px-3.5 py-2 text-xs font-semibold text-label disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0";
const dangerGhost =
  "min-h-11 rounded-lg border border-callout-border bg-card px-3.5 py-2 text-xs font-semibold text-danger disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0";
const dangerPrimary =
  "min-h-11 flex-1 rounded-lg bg-danger px-3.5 py-2 text-xs font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0";
const box = "mt-3 rounded-[10px] border border-hairline bg-card p-3";
const alertLine = "mt-2 text-xs leading-snug text-danger";
// Colourless: every line below adds its own colour, for the same
// two-utilities-one-property reason as the buttons.
const line = "mt-1.5 text-[12.5px] leading-[1.5]";

export function ProposalCard({ proposal, targetNumber, onAgree, onPark, onWithdraw }: ProposalCardProps) {
  const [discussing, setDiscussing] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [note, setNote] = useState("");
  // ONE busy value for the card: nothing here can start a second write while
  // one is in flight. Each action keeps its own error line, because the
  // sentence belongs under the button that produced it.
  const [busy, setBusy] = useState<Action | null>(null);
  const [errors, setErrors] = useState({ agree: "", park: "", withdraw: "" });

  // A REFUSING default: doc.proposals excludes accepted and withdrawn in SQL,
  // so a fourth status here is a row nothing wrote. Returning no card beats
  // half a card under a guessed title -- and the schema's status enum stays
  // four-valued, because a write response carries the row at the status it now
  // holds and nothing renders that field.
  let title: string;
  switch (proposal.status) {
    case "pending":
      title = AGREEMENT_COPY.pendingTitle(proposal.awaitingNames);
      break;
    case "parked":
      title = AGREEMENT_COPY.parkedTitle(proposal.awaitingNames);
      break;
    default:
      return null;
  }

  async function run(action: Action, write: () => Promise<string | null>): Promise<boolean> {
    setBusy(action);
    setErrors((prev) => ({ ...prev, [action]: "" }));
    const message = await write();
    setBusy(null);
    if (message !== null) setErrors((prev) => ({ ...prev, [action]: message }));
    return message === null;
  }

  async function park() {
    // Only a landed park closes the panel and clears the note: on a failure the
    // typed sentence is still there to send again.
    if (await run("park", () => onPark(proposal.id, note))) {
      setDiscussing(false);
      setNote("");
    }
  }

  // canAgree ALONE decides the Agree button. It already carries
  // `!locked && (!signedByViewer || awaitingNames is empty)`, so
  // `awaitingNames.length === 0 || canAgree` would offer Agree on a locked
  // household whose every write refuses with 409 (decisions 3 and 16).
  const everyoneAgreed = proposal.canAgree && proposal.awaitingNames.length === 0;
  // No un-park: parking says where the conversation goes, not a state to undo
  // before signing. And nobody left to wait for means nothing left to discuss.
  const showDiscuss = proposal.canAgree && proposal.status === "pending" && proposal.awaitingNames.length > 0;
  const staleNote = proposal.canWithdraw
    ? AGREEMENT_COPY.staleMine
    : proposal.proposedByName === ""
      ? AGREEMENT_COPY.staleNoName
      : AGREEMENT_COPY.staleTheirs(proposal.proposedByName);

  return (
    <div
      data-testid={`agreement-proposal-${proposal.id}`}
      className="rounded-xl border border-callout-border bg-callout px-5 py-[18px]"
    >
      <p className="text-[13px] font-semibold text-accent">{title}</p>
      <p className={`${line} text-ink`}>
        {proposalSummary(proposal.kind, proposal.proposedByName, proposal.sectionName, targetNumber)}
        <span className="text-muted"> · {agreementDateLabel(proposal.proposedAt)}</span>
      </p>
      {/* Which of previousBody/body renders is the kind's own shape on the wire
          -- an add carries no previousBody, a remove no body -- so this reads
          the fields. Only the strike-through asks the kind: an edit is the one
          card that shows both wordings, which is the only place a signer sees
          what they are agreeing to change. */}
      {proposal.previousBody !== "" && (
        <p
          data-testid="proposal-previous-body"
          className={proposal.kind === "edit" ? `${line} text-muted line-through` : `${line} text-ink`}
        >
          {proposal.previousBody}
        </p>
      )}
      {proposal.body !== "" && (
        <p data-testid="proposal-body" className={`${line} text-ink`}>
          {proposal.body}
        </p>
      )}
      {proposal.note !== "" && <p className={`${line} text-muted`}>{proposal.note}</p>}
      {/* An empty park note is ordinary, not missing data: Discuss is a bare
          button and decision 7 stores whatever was typed, including nothing. */}
      {proposal.status === "parked" && proposal.parkNote !== "" && (
        <p data-testid="proposal-park-note" className={`${line} text-muted`}>
          <span className="font-semibold text-ink">{AGREEMENT_COPY.toDiscuss}</span> {proposal.parkNote}
        </p>
      )}
      {proposal.targetChanged && (
        <p data-testid="proposal-stale-note" className={`${line} text-danger`}>
          {staleNote}
        </p>
      )}
      {everyoneAgreed && (
        <p data-testid="proposal-everyone-agreed" className={`${line} text-muted`}>
          {AGREEMENT_COPY.everyoneAgreed}
        </p>
      )}
      <div className="mt-3 flex flex-wrap gap-2">
        {proposal.canAgree && (
          <button
            type="button"
            disabled={proposal.targetChanged || busy !== null}
            onClick={() => void run("agree", () => onAgree(proposal.id))}
            className={primary}
          >
            {AGREEMENT_COPY.agree}
          </button>
        )}
        {showDiscuss && (
          <button type="button" disabled={busy !== null} onClick={() => setDiscussing(true)} className={secondary}>
            {AGREEMENT_COPY.discuss}
          </button>
        )}
        {proposal.canWithdraw && !confirming && (
          <button type="button" disabled={busy !== null} onClick={() => setConfirming(true)} className={dangerGhost}>
            {AGREEMENT_COPY.withdraw}
          </button>
        )}
      </div>
      {errors.agree !== "" && (
        <p role="alert" className={alertLine}>
          {errors.agree}
        </p>
      )}
      {/* Discuss expands HERE, inside the card, because decision 7 stores a
          park note that a bare button would leave empty every time. */}
      {discussing && (
        <div data-testid="proposal-discuss" className={box}>
          <label htmlFor={`park-note-${proposal.id}`} className="text-xs text-label">
            {AGREEMENT_COPY.parkNoteLabel}
          </label>
          {/* maxLength mirrors MaxAgreementParkNoteLen as a courtesy only: the
              browser counts UTF-16 units and the server's runes decide. */}
          <textarea
            id={`park-note-${proposal.id}`}
            value={note}
            onChange={(event) => setNote(event.target.value)}
            rows={3}
            maxLength={500}
            className="mt-1 w-full rounded-lg border border-hairline bg-card p-2 text-[13px] text-ink"
          />
          <div className="mt-2.5 flex gap-2.5">
            <button type="button" onClick={() => setDiscussing(false)} className={`${secondary} flex-1`}>
              {AGREEMENT_COPY.cancel}
            </button>
            <button type="button" disabled={busy !== null} onClick={() => void park()} className={`${primary} flex-1`}>
              {AGREEMENT_COPY.parkAction}
            </button>
          </div>
          {errors.park !== "" && (
            <p role="alert" className={alertLine}>
              {errors.park}
            </p>
          )}
        </div>
      )}
      {/* The same two-button confirm RetroModal.tsx:633-671 uses, never
          window.confirm: a browser dialog cannot be styled, cannot be tested
          without stubbing a global, and blocks the tab it opens on. */}
      {confirming && (
        <div data-testid="proposal-withdraw-confirm" className={box}>
          <p className="text-[12.5px] text-ink">{AGREEMENT_COPY.withdrawConfirmBody}</p>
          <div className="mt-2.5 flex gap-2.5">
            <button type="button" onClick={() => setConfirming(false)} className={`${secondary} flex-1`}>
              {AGREEMENT_COPY.cancel}
            </button>
            <button
              type="button"
              disabled={busy !== null}
              onClick={() => void run("withdraw", () => onWithdraw(proposal.id))}
              className={dangerPrimary}
            >
              {AGREEMENT_COPY.withdrawConfirmAction}
            </button>
          </div>
          {errors.withdraw !== "" && (
            <p role="alert" className={alertLine}>
              {errors.withdraw}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 5: Run the card tests and watch them pass**

```bash
cd web && npx vitest run src/features/marriage/ProposalCard.test.tsx
```

Expected: PASS — **14 cases** (5 matrix rows + the refusing status + the pending title and edit wordings + the everyone-agreed sentence + the park note + 3 stale rows + Discuss + Withdraw).

- [ ] **Step 6: Write the failing page tests for the mount**

A card nothing renders is a card nobody sees. Three cases in `AgreementsPage.test.tsx`, on Task 10's `renderPage`/`documentFixture`/`proposalFixture`/`ONE_OWNER` and Task 11's `sectionFixture`:

```tsx
  // Above the sections grid, not the right column the design draws: below `lg`
  // there is one column, and this card must render on a household with no
  // sections at all. targetNumber is the document's own numbering, derived
  // here at render (decision 11) -- the wire carries none.
  it("mounts a card per open proposal above the grid, numbering the target as the document numbers it", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 200,
        body: {
          agreements: documentFixture({
            sections: [sectionFixture("Money", 1, ["One shared account", "Each gets S$200/mo. no questions asked."])],
            proposals: [
              proposalFixture({
                id: "p-9",
                kind: "edit",
                targetAgreementId: "Money-1",
                previousBody: "Each gets S$200/mo. no questions asked.",
                body: "Each gets S$250/mo. no questions asked.",
                canAgree: true,
              }),
            ],
          }),
        },
      },
    });

    const card = await screen.findByTestId("agreement-proposal-p-9");
    expect(card).toHaveTextContent("Andreas proposed changing Money 02:");
    // The block, not the card, is what must precede the grid -- so this stays
    // true if a card is ever nested one level deeper inside it.
    const grid = screen.getByTestId("agreements-column-0");
    const block = screen.getByTestId("agreements-proposals");
    expect(block.compareDocumentPosition(grid) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  // The wiring this case exists for: agree -> the hook -> handleWriteError.
  // handleWriteError is synchronous -- it fires reload() and returns the
  // sentence without awaiting the refetch -- so the alert lands before the
  // second GET does, and the disabled Agree needs a wait of its own. That
  // second GET comes from reload(), NOT from the hook's afterWrite(): the
  // mutation failed, so its onSuccess never ran.
  it("a refused agree shows the refusal in the card, and the refetched document disables Agree", async () => {
    const open = proposalFixture({ id: "p-9", canAgree: true });
    renderPage({
      [`GET ${DOC_URL}`]: [
        { status: 200, body: { agreements: documentFixture({ proposals: [open] }) } },
        { status: 200, body: { agreements: documentFixture({ proposals: [{ ...open, targetChanged: true }] }) } },
      ],
      [`POST ${DOC_URL}/proposals/p-9/agree`]: {
        status: 409,
        body: { error: { code: "AGREEMENT_CHANGED", message: "The target changed." } },
      },
    });

    fireEvent.click(await screen.findByRole("button", { name: "Agree" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "This no longer matches the agreement it was written against, so nothing was signed.",
    );
    await waitFor(() => expect(screen.getByRole("button", { name: "Agree" })).toBeDisabled());
  });

  // Decision 3's other half: the locked document keeps its frozen proposals and
  // offers no write control at all -- Agree, Discuss and Withdraw are gone, not
  // disabled. canAgree/canWithdraw are the server's own flags, and a one-owner
  // household's awaiting set is empty because the other owner has left.
  it("a locked household lists its frozen proposals with no write control", async () => {
    renderPage({
      [`GET ${DOC_URL}`]: {
        status: 200,
        body: {
          agreements: documentFixture({
            locked: true,
            owners: ONE_OWNER,
            proposals: [proposalFixture({ id: "p-9", awaitingNames: [], canAgree: false, canWithdraw: false })],
          }),
        },
      },
    });

    expect(await screen.findByTestId("agreement-proposal-p-9")).toHaveTextContent("Pending change");
    expect(screen.queryByRole("button", { name: "Agree" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Discuss" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Withdraw" })).not.toBeInTheDocument();
  });
```

Task 10's own locked-with-content case uses `proposalFixture()`'s defaults, which include `canAgree: true` — a combination the server never sends to a locked household. It stays green either way; this new case is the one that pins the rule.

- [ ] **Step 7: Run them and watch them fail**

```bash
cd web && npx vitest run src/features/marriage/AgreementsPage.test.tsx
```

Expected: FAIL — all three new cases report `TestingLibraryElementError: Unable to find an element by: [data-testid="agreement-proposal-p-9"]`. A **test** failure: the card exists and nothing mounts it. Every earlier case in the file, Task 11's included, stays green.

- [ ] **Step 8: Mount the block on the page**

In `AgreementsPage.tsx`, add the import beside Task 11's:

```tsx
import { ProposalCard } from "./ProposalCard";
```

Then, in the answered-query branch beside Task 11's `visible`/`columns` constants:

```tsx
  // The display number of whatever an edit or a remove targets, as the
  // document numbers it right now -- null when no live agreement carries that
  // id, which is the targetChanged case and the reason the card can say so.
  // Composed here, from the same array the grid renders, so a card and a row
  // can never disagree about what "Money 02" means.
  const liveAgreements = doc.sections.flatMap((section) => section.agreements);
  const targetNumberOf = (targetAgreementId: string) =>
    liveAgreements.find((agreement) => agreement.id === targetAgreementId)?.number ?? null;
```

and, inside the `<PageContainer>`, after the four state panels and **before** Task 11's grid:

```tsx
      {/* One block above the sections, not the design's right column: below
          `lg` there is one column, and a household with no sections at all
          still has to see what is waiting for it. Every write goes through
          handleWriteError, which owns the refetch that turns a 409 into a
          card that explains itself -- so each handler resolves to null when
          the write landed, or to the sentence the card shows. */}
      {doc.proposals.length > 0 && (
        <div data-testid="agreements-proposals" className="flex flex-col gap-4">
          {doc.proposals.map((proposal) => (
            <ProposalCard
              key={proposal.id}
              proposal={proposal}
              targetNumber={targetNumberOf(proposal.targetAgreementId)}
              onAgree={(id) =>
                agreements
                  .agree(id)
                  .then(() => null)
                  .catch((err: unknown) => handleWriteError(err, agreements.reload, AGREEMENT_COPY.agreeError))
              }
              onPark={(id, note) =>
                agreements
                  .park(id, note)
                  .then(() => null)
                  .catch((err: unknown) => handleWriteError(err, agreements.reload, AGREEMENT_COPY.parkError))
              }
              onWithdraw={(id) =>
                agreements
                  .withdraw(id)
                  .then(() => null)
                  .catch((err: unknown) => handleWriteError(err, agreements.reload, AGREEMENT_COPY.withdrawError))
              }
            />
          ))}
        </div>
      )}
```

`agreements.reload` is passed by reference: `handleWriteError`'s second parameter is `() => Promise<void>`, which is exactly what the hook returns, so no arrow wrapper is needed and none should be added.

- [ ] **Step 9: Run the page tests and watch them pass**

```bash
cd web && npx vitest run src/features/marriage
```

Expected: PASS — the 3 new `AgreementsPage.test.tsx` cases, the 13 in `ProposalCard.test.tsx`, and every earlier case in `src/features/marriage` still green.

- [ ] **Step 10: Mutation-check the matrix, the Discuss gate and the error wiring**

**Agree's gate.** In `ProposalCard.tsx`, replace `{proposal.canAgree && (` on the Agree button with the naive reading of the design's first matrix row:

```tsx
        {(proposal.canAgree || proposal.awaitingNames.length === 0) && (
```

Expected: **test** failure. `offers the right actions to a locked household whose awaiting list emptied` goes red on `expect(screen.queryByRole("button", { name: "Agree" })).not.toBeInTheDocument()`, jest-dom reporting that the document was expected not to contain the element and a `<button>` was found. The other four matrix rows stay green, which is what proves that fixture is the *locked* one and not just any empty list. Restore.

**The Discuss gate.** Delete `&& proposal.awaitingNames.length > 0` from `showDiscuss`. Expected: **test** failure. `offers the right actions to everyone still here has already agreed` goes red the same way on `Discuss`. Restore.

**The error wiring.** In `AgreementsPage.tsx`, drop the `.catch(...)` from `onAgree`, leaving `onAgree={(id) => agreements.agree(id).then(() => null)}`. Expected: **test** failure. `a refused agree shows the refusal in the card` goes red with `Unable to find an accessible element with the role "alert"` after its timeout, and Vitest may report an unhandled rejection from the card's `run` alongside it — either is the red; say in the commit body which you saw. Restore.

- [ ] **Step 11: Commit**

```bash
cd web && npx vitest run && cd .. && make lint
git add web/src/features/marriage/ProposalCard.tsx \
        web/src/features/marriage/ProposalCard.test.tsx \
        web/src/features/marriage/agreementCopy.ts \
        web/src/features/marriage/AgreementsPage.tsx \
        web/src/features/marriage/AgreementsPage.test.tsx
git commit -m "feat(agreements): the proposal card, and an action matrix the server decides

Three mutation checks, all three test failures rather than build failures:

- Widened Agree's gate to (canAgree || awaitingNames.length === 0), the naive
  reading of the design's first matrix row. 'offers the right actions to a
  locked household whose awaiting list emptied' went red -- a button named
  Agree was in a document that expected none -- while the other four matrix
  rows stayed green.
- Deleted '&& awaitingNames.length > 0' from showDiscuss. 'offers the right
  actions to everyone still here has already agreed' went red on Discuss.
- Dropped the .catch that runs a failed agree through handleWriteError.
  'a refused agree shows the refusal in the card' went red with 'Unable to
  find an accessible element with the role alert', plus an unhandled
  rejection reported by Vitest from the card's own run().

The build failure this task also saw was the expected one at Step 2:
Failed to resolve import ./ProposalCard, before the file existed."
```

---

### Task 13: The Propose modal

**Files:**
- Create: `web/src/features/marriage/ProposeAgreementModal.tsx`, `web/src/features/marriage/ProposeAgreementModal.test.tsx`
- Modify: `web/src/features/marriage/agreementCopy.ts` (append inside `AGREEMENT_COPY`), `web/src/features/marriage/AgreementsPage.tsx` (the mount only)

**Interfaces:**

Consumes — every signature copied from the plan header's "Interfaces, in one place". Invent none of these names.

```ts
// web/src/features/marriage/useAgreements.ts (Task 9)
useAgreements(): {
  data: AgreementsDocument | undefined
  isLoading: boolean
  error: unknown
  reload: () => Promise<void>
  createSection: (name: string) => Promise<{ section: AgreementSection; agreements: AgreementsDocument }>
  seedStarterSet: () => Promise<AgreementsDocument>
  propose: (body: ProposeBody) => Promise<AgreementsDocument>
  agree: (proposalId: string) => Promise<AgreementsDocument>
  park: (proposalId: string, note: string) => Promise<AgreementsDocument>
  withdraw: (proposalId: string) => Promise<AgreementsDocument>
  isProposing: boolean
  isCreatingSection: boolean
}
handleWriteError(err: unknown, reload: () => Promise<void>, fallback: string): string

// ProposeBody is declared beside the hook. This file never imports the name:
// `propose()` takes an object literal and TypeScript infers it.
type ProposeBody = {
  kind: "add" | "edit" | "remove"
  sectionId: string
  targetAgreementId: string
  body: string
  previousBody: string
  note: string
}

// web/src/features/marriage/agreementSchemas.ts (Task 9)
type AgreementSection = AgreementsDocument["sections"][number]
//   = { id: string; name: string; count: number; visible: boolean
//       agreements: { id: string; number: number; body: string }[] }

// web/src/features/marriage/agreementCopy.ts (Task 9)
AGREEMENT_COPY            // the one copy object; this task appends keys to it
joinNames(names: string[]): string   // joins with " and " -- used by the copy
                                     // functions below, inside that module

// web/src/components/Modal.tsx, web/src/api/client.ts
Modal(props: { open: boolean; onClose: () => void; title: string; children: ReactNode; wide?: boolean })
class ApiError extends Error { readonly code: string; readonly status: number }
```

Produces — Tasks 14 and 15 both hand `AgreementsPage` an `AgreementProposeSeed`; nothing else imports this file.

```ts
export type AgreementProposeSeed = {
  mode: "add" | "edit" | "remove"
  sectionId?: string
  targetAgreementId?: string
  body?: string
}

export function ProposeAgreementModal(props: {
  seed: AgreementProposeSeed
  coOwnerNames: string[]
  sections: AgreementSection[]
  onOpenNewSection: () => void
  onClose: () => void
}): JSX.Element
```

Two things this task does **not** do, because the plan header's Conventions give them to Task 10: the header's **Propose a change** button, and hiding it when `doc.locked`. Task 10 lands `AgreementsPage.tsx` complete with `useMe()`, the three modal booleans (`proposeSeed`, `newSectionOpen`, `historyOpen`) and the three header buttons that set them. This task adds the render and nothing else, so nothing here depends on Task 14 (which owns `NewSectionModal`, not the state that opens it).

- [ ] **Step 1: Write the failing tests** — Create `web/src/features/marriage/ProposeAgreementModal.test.tsx`. Three mechanics hold in this task and in Tasks 14 and 15: **`fireEvent`, never `userEvent`** (`@testing-library/user-event` is not in `web/package.json` and Global Constraints forbid a new dependency — `fireEvent.change` replaces `userEvent.type`); **per-route `capture`** for request bodies (`stub.bodyOf` does not exist — see `web/src/test/fetchStub.ts`); and **copy asserted as literals**, never imported from `agreementCopy.ts`, so a typo there cannot make its own test pass.

```tsx
// renderWithRouter, not a bare render: it is what supplies the
// QueryClientProvider useAgreements() needs (and a router for anything that
// ever grows a <Link>). Every fixture below is WRAPPED -- the wire is
// { "agreements": {…} } and { "proposal": {…}, "agreements": {…} } -- because
// the schemas parse the envelope and return the inner object.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { ProposeAgreementModal, type AgreementProposeSeed } from "./ProposeAgreementModal";

const SECTIONS = [
  {
    id: "s-money",
    name: "Money",
    count: 1,
    visible: true,
    agreements: [{ id: "a-1", number: 1, body: "Each gets S$200/mo no-questions-asked" }],
  },
];

const OWNERS = [
  { membershipId: "m-a", name: "Andreas" },
  { membershipId: "m-c", name: "Christine" },
];

const doc = (version: number) => ({
  agreements: {
    locked: false,
    owners: OWNERS,
    version,
    updatedAt: null,
    sections: SECTIONS,
    proposals: [],
    history: [],
  },
});

function renderModal(
  seed: AgreementProposeSeed,
  routes: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  stubFetchRoutes({
    "GET /api/v1/marriage/agreements": { status: 200, body: doc(2) },
    ...routes,
  });
  return renderWithRouter(
    <ProposeAgreementModal
      seed={seed}
      coOwnerNames={["Christine"]}
      sections={SECTIONS}
      onOpenNewSection={() => {}}
      onClose={() => {}}
    />,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ProposeAgreementModal", () => {
  // Real radios, checked from the seed and never from a hardcoded "edit"
  // (spec, The modals). RetroModal.tsx's mood picker is the shape: visible
  // inputs sharing one `name`, NOT sr-only inputs behind a styled stand-in --
  // that shape shipped keyboard-invisible focus once already, and
  // fireEvent.click never presses a key, so no unit test can catch it.
  it("the chips are three real radios checked from the seed, and remove warns", async () => {
    renderModal({ mode: "remove", targetAgreementId: "a-1" });

    const group = await screen.findByRole("radiogroup", { name: "Change type" });
    expect(within(group).getAllByRole("radio")).toHaveLength(3);
    expect(screen.getByRole("radio", { name: "Remove" })).toBeChecked();
    // "Why (optional)" renders in all three modes (spec, The modals).
    expect(screen.getByLabelText("Why (optional)")).toBeInTheDocument();
    expect(screen.getByTestId("agreement-remove-warning")).toHaveTextContent(
      'This will be removed once Christine agrees. It stays in Version history, ' +
        "so you can always see it was there and restore it later.",
    );
  });

  // Two claims in one flow, because they are the same defect from both sides:
  // an edit pre-fills its wording, and switching mode CLEARS it. A hidden
  // stale value that still submits is worse than an empty one. The send is
  // asserted with toEqual against the WHOLE body -- toMatchObject would pass
  // a propose that dropped its note.
  it("edit pre-fills the wording, switching mode clears it, and the send carries all six fields", async () => {
    let sent: unknown;
    renderModal(
      { mode: "edit", targetAgreementId: "a-1" },
      {
        "POST /api/v1/marriage/agreements/proposals": {
          status: 201,
          body: {
            proposal: {
              id: "p-1",
              kind: "add",
              status: "pending",
              sectionId: "s-money",
              sectionName: "Money",
              targetAgreementId: "",
              body: "Screens off at meals",
              previousBody: "",
              note: "The table is for us",
              parkNote: "",
              proposedByMembershipId: "m-a",
              proposedByName: "Andreas",
              proposedAt: "2026-09-05T09:00:00Z",
              awaitingNames: ["Christine"],
              targetChanged: false,
              canAgree: false,
              canWithdraw: true,
            },
            agreements: doc(2).agreements,
          },
          capture: (body) => {
            sent = body;
          },
        },
      },
    );

    expect(await screen.findByLabelText("New wording")).toHaveValue(
      "Each gets S$200/mo no-questions-asked",
    );

    fireEvent.click(screen.getByRole("radio", { name: "Add new" }));
    expect(screen.getByLabelText("New agreement")).toHaveValue("");

    fireEvent.change(screen.getByLabelText("New agreement"), {
      target: { value: "Screens off at meals" },
    });
    fireEvent.change(screen.getByLabelText("Why (optional)"), {
      target: { value: "The table is for us" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send for agreement" }));

    await waitFor(() =>
      expect(sent).toEqual({
        kind: "add",
        sectionId: "s-money",
        targetAgreementId: "",
        body: "Screens off at meals",
        previousBody: "",
        note: "The table is for us",
      }),
    );
  });

  // The latch (decision 13; spec, "Propose answers 409 AGREEMENT_CHANGED of
  // its own"). The two GET fixtures MUST differ: identical bodies are
  // structurally shared by TanStack Query, `data` keeps its reference, and
  // Step 5's mutation would never re-run.
  it("a 409 latches Send off, and it stays off across the refetch it triggers", async () => {
    const { queryClient } = renderModal(
      { mode: "edit", targetAgreementId: "a-1" },
      {
        "GET /api/v1/marriage/agreements": [
          { status: 200, body: doc(2) },
          { status: 200, body: doc(3) },
        ],
        "POST /api/v1/marriage/agreements/proposals": {
          status: 409,
          body: { error: { code: "AGREEMENT_CHANGED", message: "This agreement changed." } },
        },
      },
    );

    fireEvent.change(await screen.findByLabelText("New wording"), {
      target: { value: "Each gets S$250/mo no-questions-asked" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Send for agreement" }));

    expect(await screen.findByTestId("agreement-propose-conflict")).toHaveTextContent(
      "This agreement changed while you were writing, so nothing was saved.",
    );

    // The refetch must have LANDED, not merely been requested: a route's
    // `capture` fires when the request is ISSUED, so counting requests would
    // run the next line one commit too early -- which is exactly the commit
    // in which Step 5's mutation clears the latch, leaving the check green
    // against broken code. Reading the cache also means this file never
    // imports Task 9's query-key name.
    await waitFor(() =>
      expect(
        queryClient
          .getQueryCache()
          .getAll()
          .map((query) => query.state.data),
      ).toContainEqual(expect.objectContaining({ version: 3 })),
    );

    expect(screen.getByRole("button", { name: "Send for agreement" })).toBeDisabled();
    // Nothing typed is lost -- criterion 10's half that jsdom can express.
    expect(screen.getByLabelText("New wording")).toHaveValue(
      "Each gets S$250/mo no-questions-asked",
    );
  });
});
```

- [ ] **Step 2: Run them and watch them fail** — `cd web && npx vitest run src/features/marriage/ProposeAgreementModal.test.tsx`

Expected: a **collection (build) failure, not a test failure** — Vitest never runs a case, and reports `Test Files 1 failed (1)` with `Tests no tests` above it:

```
Error: Failed to resolve import "./ProposeAgreementModal" from
"src/features/marriage/ProposeAgreementModal.test.tsx". Does the file exist?
```

Say which of the two you saw in Step 6's commit body. Here it is the build failure; in Step 5 it must be a test failure, and that difference is the whole point of recording it.

- [ ] **Step 3: Build the modal** — First append this task's copy **inside the existing `AGREEMENT_COPY` object** in `agreementCopy.ts`. One copy module, one object (plan header, Conventions): a second export is not allowed, and a duplicate key is a TypeScript error.

Before typing, run `grep -n "cancel:" web/src/features/marriage/agreementCopy.ts` — Task 12's Discuss composer may already have added `cancel: "Cancel"`. If it is there, reuse it and do **not** add a second.

```ts
  // --- Propose modal (Task 13) ---
  proposeTitle: "Propose a change",
  changeTypeLabel: "Change type",
  modeAdd: "Add new",
  modeEdit: "Edit existing",
  modeRemove: "Remove",
  // `cancel` is NOT repeated here: Task 12 already added it to this object and a
  // duplicate key is a TypeScript error. `grep -n 'cancel:' agreementCopy.ts`
  // before typing, which is the rule for every key this task appends.
  addSectionLabel: "Add to section",
  newSectionLink: "+ New section",
  addBodyLabel: "New agreement",
  addBodyPlaceholder: "e.g. Screens off during family meals, no exceptions.",
  editTargetLabel: "Which agreement to edit",
  editBodyLabel: "New wording",
  removeTargetLabel: "Which agreement to remove",
  proposeNoteLabel: "Why (optional)",
  proposeSend: "Send for agreement",
  // "will be asked" agrees with any count, so this one needs no branch.
  proposeSubtitle: (names: string[]) =>
    `${joinNames(names)} will be asked to agree before it takes effect`,
  proposeNotePlaceholder: (names: string[]) => `Add a note for ${joinNames(names)}…`,
  // The design's warning verbatim but for the names. This is the one place
  // the verb has to agree with the count, because joinNames alone yields
  // "…Christine agree".
  removeWarning: (names: string[]) =>
    `This will be removed once ${joinNames(names)} ${names.length === 1 ? "agrees" : "agree"}. ` +
    "It stays in Version history, so you can always see it was there and restore it later.",
  // Rendered only when the seed pre-filled an add body. Version history's
  // Restore is the one entry point that does (decision 18).
  restoreNote: "Restoring is an ordinary proposal — it takes effect once everyone agrees.",
  proposeConflict:
    "This agreement changed while you were writing, so nothing was saved. " +
    "Close this and start again from the current wording.",
  proposeFallbackError: "Could not send that for agreement. Try again.",
  // The full body, never truncated: the design truncates but names no rule,
  // and inventing one risks two agreements sharing a label. The zero padding
  // is presentation, which is why the wire carries an integer (decision 11).
  targetOption: (number: number, body: string) => `${String(number).padStart(2, "0")} · ${body}`,
```

Then create `web/src/features/marriage/ProposeAgreementModal.tsx`:

```tsx
// The three-mode editor behind every entry point that proposes a change: the
// header's "Propose a change", the empty state's "Add your first agreement",
// New section's "Create & add first agreement" and Version history's Restore.
// All four hand it one seed -- { mode, sectionId?, targetAgreementId?, body? },
// every field a pre-fill -- so there is one editor here and not four.
//
// It is the only component in this feature that holds a draft, which is why
// the `hadConflict` latch lives here and nowhere else, and why AgreementsPage
// renders it only while it is open rather than keeping it mounted behind
// `open={false}`: unmounting is the only way back to a writable send button.
import { useId, useState } from "react";
import { Modal } from "../../components/Modal";
import { ApiError } from "../../api/client";
import { AGREEMENT_COPY } from "./agreementCopy";
import { handleWriteError, useAgreements } from "./useAgreements";
import type { AgreementSection } from "./agreementSchemas";

// The pre-fill, not the request: `mode` becomes the request's `kind`, and the
// three optional fields are whatever the caller already knows. AgreementsPage
// holds one of these in state; Tasks 14 and 15 hand it one.
export type AgreementProposeSeed = {
  // AgreementKind, not a second literal union: two unions for one server enum
  // is how the two drift apart.
  mode: AgreementKind;
  sectionId?: string;
  targetAgreementId?: string;
  body?: string;
};

const MODES: { value: AgreementProposeSeed["mode"]; label: string }[] = [
  { value: "add", label: AGREEMENT_COPY.modeAdd },
  { value: "edit", label: AGREEMENT_COPY.modeEdit },
  { value: "remove", label: AGREEMENT_COPY.modeRemove },
];

const LABEL_CLASS = "text-xs font-semibold text-label";
const TEXTAREA_CLASS =
  "min-h-20 rounded-[10px] border border-hairline bg-card px-3.5 py-3 text-[13px] leading-relaxed";
// w-full min-w-0 is load-bearing, not cosmetic: a <select> sizes itself to its
// longest <option>, and an option here carries an agreement's full body (up to
// 500 characters). SignInScreen.tsx:231 records the same defect measured in a
// real browser -- one long currency option held the sign-up card at 428px on a
// 375px phone and the page scrolled sideways.
const SELECT_CLASS =
  "min-h-11 w-full min-w-0 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0";

export function ProposeAgreementModal({
  seed,
  coOwnerNames,
  sections,
  onOpenNewSection,
  onClose,
}: {
  seed: AgreementProposeSeed;
  // Whose names the subtitle and the remove warning print. Names, never
  // permission: Agree and Withdraw read the server's own canAgree/canWithdraw.
  coOwnerNames: string[];
  // Every section, empty ones included: the document hides an empty section
  // (decision 8) and this picker still offers it, which is what makes Restore
  // into a section that has since emptied work at all.
  sections: AgreementSection[];
  onOpenNewSection: () => void;
  onClose: () => void;
}) {
  const { propose, isProposing, reload } = useAgreements();

  const modeLegendId = useId();
  const sectionSelectId = useId();
  const targetSelectId = useId();
  const bodyFieldId = useId();
  const noteFieldId = useId();

  // Every live agreement, in document order, flattened once: the target select
  // offers these and `bodyOf` reads them.
  const targets = sections.flatMap((section) => section.agreements);

  // One agreement's current wording, by id. The edit pre-fill and the
  // `previousBody` this modal sends both read it, and they must read the same
  // thing -- the server compares `previousBody` against the live row and
  // refuses a mismatch (decision 13), so wording the proposer never saw must
  // never be what gets compared.
  function bodyOf(agreementId: string): string {
    return targets.find((agreement) => agreement.id === agreementId)?.body ?? "";
  }

  // A select whose state is "" while it displays its first option is the dead
  // end docs/LEARNING.md records for BillsPage: what you see is not what gets
  // sent. So edit and remove start on a real agreement, never on nothing.
  const initialTargetId =
    seed.mode === "add" ? "" : (seed.targetAgreementId ?? targets[0]?.id ?? "");

  const [mode, setMode] = useState<AgreementProposeSeed["mode"]>(seed.mode);
  const [sectionId, setSectionId] = useState(seed.sectionId ?? sections[0]?.id ?? "");
  const [targetId, setTargetId] = useState(initialTargetId);
  // Seeded from the seed's own body (Restore's pre-fill) or, on an edit, from
  // the target's current wording -- "New wording, pre-filled with the current
  // body", so nobody retypes what is already there.
  const [body, setBody] = useState(
    seed.body ?? (seed.mode === "edit" ? bodyOf(initialTargetId) : ""),
  );
  const [note, setNote] = useState("");
  const [error, setError] = useState<string | null>(null);
  // One-way, component-local, set from err.code -- never useAgreements' own
  // state, which clears on the next background refetch and would re-enable
  // Send over wording that has since moved. RetroModal.tsx:101 and
  // VisionModal.tsx:470 are the same latch for the same defect: once true it
  // never goes false again for this mount's life, and the way back is closing
  // the modal, which unmounts this component and its draft together.
  const [hadConflict, setHadConflict] = useState(false);

  // Clears the OTHER mode's inputs. A hidden stale value that still submits is
  // worse than an empty one (spec, The modals).
  function chooseMode(next: AgreementProposeSeed["mode"]) {
    const nextTargetId = next === "add" ? "" : (targets[0]?.id ?? "");
    setMode(next);
    setTargetId(nextTargetId);
    setBody(next === "edit" ? bodyOf(nextTargetId) : "");
  }

  function chooseTarget(nextTargetId: string) {
    setTargetId(nextTargetId);
    // Only an edit pre-fills; a remove sends no body at all.
    if (mode === "edit") setBody(bodyOf(nextTargetId));
  }

  // An edit or a remove with no target selects nothing to change, and an add
  // or an edit with no wording proposes nothing. Both are shapes the server
  // refuses with ErrAgreementProposalShapeInvalid, so Send stays off rather
  // than sending a request that can only 422. The empty select above it is
  // what says why: a household with no agreements at all cannot edit one.
  const canSend =
    mode === "add"
      ? body.trim() !== ""
      : targetId !== "" && (mode === "remove" || body.trim() !== "");

  async function handleSend() {
    setError(null);
    try {
      await propose({
        kind: mode,
        // The handler blanks the section for anything but an add anyway (the
        // section is the target's), and Validate refuses a caller-supplied one
        // as its fail-closed backstop. Sending "" keeps the two sides agreeing.
        sectionId: mode === "add" ? sectionId : "",
        targetAgreementId: mode === "add" ? "" : targetId,
        body: mode === "remove" ? "" : body.trim(),
        previousBody: mode === "add" ? "" : bodyOf(targetId),
        note: note.trim(),
      });
      onClose();
    } catch (err) {
      // handleWriteError first, always: it is behaviour, not copy -- it is
      // what calls reload(), and the document has moved under this draft
      // whichever refusal came back. Its return value is the message for the
      // failures it does not name itself.
      const message = handleWriteError(err, reload, AGREEMENT_COPY.proposeFallbackError);
      if (err instanceof ApiError && err.code === "AGREEMENT_CHANGED") {
        // The conflict has its own banner, which says more than one line can:
        // stay open, keep what is typed, start again from the current wording.
        // Setting `error` too would print the same refusal twice.
        setHadConflict(true);
        return;
      }
      setError(message);
    }
  }

  return (
    <Modal open onClose={onClose} title={AGREEMENT_COPY.proposeTitle}>
      <p className="-mt-2 mb-4 text-xs text-muted">
        {AGREEMENT_COPY.proposeSubtitle(coOwnerNames)}
      </p>

      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          // The form submits nothing. RetroModal.tsx:355-365 is the same guard
          // for the same reason: a `type="submit"` once let Enter anywhere in
          // a form finish a retro. Enter here would send a proposal both
          // owners then have to live with.
          event.preventDefault();
        }}
      >
        <div>
          <h3 id={modeLegendId} className={`mb-2.5 ${LABEL_CLASS}`}>
            {AGREEMENT_COPY.changeTypeLabel}
          </h3>
          {/* Three real radios sharing one `name`: one tab stop, arrow keys
              between options, a visible focus ring on a real on-screen
              element -- all free from the platform. NOT sr-only inputs behind
              a styled stand-in, which is the shape that shipped
              keyboard-invisible focus in TransactionsPage's Kind filter
              (docs/LEARNING.md pattern 3) with every unit test green, because
              fireEvent.click never presses a key. RetroModal.tsx:400-413 is
              the corrected shape this copies. */}
          <div role="radiogroup" aria-labelledby={modeLegendId} className="flex gap-1">
            {MODES.map((option) => (
              <label
                key={option.value}
                className={`flex min-h-11 flex-1 cursor-pointer items-center justify-center gap-1.5 rounded-[10px] border py-2 text-[12.5px] sm:min-h-0 ${
                  mode === option.value ? "border-accent bg-callout" : "border-hairline"
                }`}
              >
                <input
                  type="radio"
                  name="agreement-mode"
                  checked={mode === option.value}
                  onChange={() => chooseMode(option.value)}
                  className="h-4 w-4 accent-accent"
                />
                {option.label}
              </label>
            ))}
          </div>
        </div>

        {mode === "add" && (
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between gap-3">
              <label htmlFor={sectionSelectId} className={LABEL_CLASS}>
                {AGREEMENT_COPY.addSectionLabel}
              </label>
              {/* Opens New section from inside this one. The page closes this
                  modal before opening that one -- nothing here stacks
                  <dialog>s -- and hands the new section straight back as a
                  fresh seed. */}
              <button
                type="button"
                onClick={onOpenNewSection}
                className="min-h-11 text-xs font-semibold text-accent sm:min-h-0"
              >
                {AGREEMENT_COPY.newSectionLink}
              </button>
            </div>
            <select
              id={sectionSelectId}
              value={sectionId}
              onChange={(event) => setSectionId(event.target.value)}
              className={SELECT_CLASS}
            >
              {sections.map((section) => (
                <option key={section.id} value={section.id}>
                  {section.name}
                </option>
              ))}
            </select>
          </div>
        )}

        {mode !== "add" && (
          <div className="flex flex-col gap-1.5">
            <label htmlFor={targetSelectId} className={LABEL_CLASS}>
              {mode === "edit"
                ? AGREEMENT_COPY.editTargetLabel
                : AGREEMENT_COPY.removeTargetLabel}
            </label>
            <select
              id={targetSelectId}
              value={targetId}
              onChange={(event) => chooseTarget(event.target.value)}
              className={SELECT_CLASS}
            >
              {targets.map((agreement) => (
                <option key={agreement.id} value={agreement.id}>
                  {AGREEMENT_COPY.targetOption(agreement.number, agreement.body)}
                </option>
              ))}
            </select>
          </div>
        )}

        {mode !== "remove" && (
          <div className="flex flex-col gap-1.5">
            <label htmlFor={bodyFieldId} className={LABEL_CLASS}>
              {mode === "add" ? AGREEMENT_COPY.addBodyLabel : AGREEMENT_COPY.editBodyLabel}
            </label>
            <textarea
              id={bodyFieldId}
              value={body}
              onChange={(event) => setBody(event.target.value)}
              placeholder={mode === "add" ? AGREEMENT_COPY.addBodyPlaceholder : undefined}
              rows={3}
              // Mirrors MaxAgreementBodyLen as a courtesy; the rune-counting
              // server is the authority. maxLength counts UTF-16 code units,
              // so an emoji costs two here and one there -- the browser is the
              // stricter of the two on that input, which is the safe direction
              // for a cap that exists only to stop someone typing 4,000
              // characters into a box the server will refuse.
              maxLength={500}
              className={TEXTAREA_CLASS}
            />
          </div>
        )}

        {mode === "remove" && (
          // The existing danger pair, as AdminMailPage.tsx:63 uses it and as
          // RetroDetail.tsx:146-152 paints the design's own warm card.
          <p
            data-testid="agreement-remove-warning"
            className="rounded-[10px] border border-danger-border bg-danger-soft p-3.5 text-[12.5px] leading-relaxed text-danger"
          >
            {AGREEMENT_COPY.removeWarning(coOwnerNames)}
          </p>
        )}

        <div className="flex flex-col gap-1.5">
          <label htmlFor={noteFieldId} className={LABEL_CLASS}>
            {AGREEMENT_COPY.proposeNoteLabel}
          </label>
          <textarea
            id={noteFieldId}
            value={note}
            onChange={(event) => setNote(event.target.value)}
            placeholder={AGREEMENT_COPY.proposeNotePlaceholder(coOwnerNames)}
            rows={2}
            maxLength={500}
            className={TEXTAREA_CLASS}
          />
        </div>

        {/* Only Restore seeds an add with a body already in it (decision 18),
            so this sentence appears exactly where restoring is what is
            happening -- and says the thing the button cannot: this is a
            proposal like any other, not a one-click undo. */}
        {seed.mode === "add" && seed.body ? (
          <p className="text-xs leading-relaxed text-muted">{AGREEMENT_COPY.restoreNote}</p>
        ) : null}

        {hadConflict && (
          <p
            data-testid="agreement-propose-conflict"
            role="alert"
            className="rounded-lg border border-danger-border bg-danger-soft px-3.5 py-2.5 text-[12.5px] leading-relaxed text-danger"
          >
            {AGREEMENT_COPY.proposeConflict}
          </p>
        )}

        {error !== null && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {error}
          </p>
        )}

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label sm:min-h-0"
          >
            {AGREEMENT_COPY.cancel}
          </button>
          {/* type="button", never "submit": see the form's onSubmit above. */}
          <button
            type="button"
            disabled={hadConflict || isProposing || !canSend}
            onClick={() => void handleSend()}
            className="min-h-11 flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          >
            {AGREEMENT_COPY.proposeSend}
          </button>
        </div>
      </form>
    </Modal>
  );
}
```

Then mount it from `AgreementsPage.tsx`. Task 10 already holds the state and the button; this adds the render.

```
grep -n "proposeSeed\|useMe(" web/src/features/marriage/AgreementsPage.tsx
```

Three edits, in this order:

1. **The seed's type.** Task 10 could not import `AgreementProposeSeed` from a file that did not exist, so its `useState` carries the structural type inline. Replace that annotation with the imported name, so one name describes the seed everywhere:

```tsx
import { ProposeAgreementModal, type AgreementProposeSeed } from "./ProposeAgreementModal";

const [proposeSeed, setProposeSeed] = useState<AgreementProposeSeed | null>(null);
```

If Task 10 imported the name from somewhere else instead, import it from there and do **not** export a second one — two modules exporting one name for one meaning is finding 1.13's class.

2. **`coOwnerNames`**, beside the page's existing `useMe()` binding (use whatever name Task 10 gave it; `me` below):

```tsx
// Whose names the modal prints, and nothing more. Permission is the server's:
// Agree and Withdraw read the stamped canAgree/canWithdraw, never a membership
// id compared in the browser (docs/LEARNING.md pattern 1).
const coOwnerNames = doc.owners
  .filter((owner) => owner.membershipId !== me.data?.membership.id)
  .map((owner) => owner.name);
```

3. **The render**, as the last child of the page's outermost returned element, where Tasks 14 and 15 will put theirs:

```tsx
{proposeSeed && (
  <ProposeAgreementModal
    seed={proposeSeed}
    coOwnerNames={coOwnerNames}
    sections={doc.sections}
    onOpenNewSection={() => {
      // Close, then open: nothing here stacks <dialog>s, and closing this one
      // is also what discards its draft and its latch.
      setProposeSeed(null);
      setNewSectionOpen(true);
    }}
    onClose={() => setProposeSeed(null)}
  />
)}
```

`newSectionOpen` already exists — Task 10 declared all three modal booleans (plan header, Conventions), so this compiles before Task 14 builds the modal that boolean will open.

- [ ] **Step 4: Run the tests and watch them pass** — `cd web && npx vitest run src/features/marriage`

Expected: PASS, the whole marriage folder included. The modal calls `useAgreements()`, which shares the page's one query key, so mounting it fires **no new request** and no existing page test needs a new stub. If `stubFetchRoutes` throws `no stub registered for "GET /api/v1/marriage/agreements"` from a Task 10 test, the modal is fetching under a key of its own — fix that, not the stub.

- [ ] **Step 5: Mutation-check the latch** — Replace the one-way latch with the two-way flag it exists to refuse. In `ProposeAgreementModal.tsx`, take `data` from the hook and clear the latch whenever the document changes, which is what reading the query hook's own state amounts to:

```tsx
const { propose, isProposing, reload, data } = useAgreements();
useEffect(() => setHadConflict(false), [data]);   // + import useEffect
```

Expected: **"a 409 latches Send off, and it stays off across the refetch it triggers"** goes red on `toBeDisabled()`:

```
AssertionError: expected element to be disabled
```

and **only** that test. Restore both lines afterwards (the destructured `data` too — an unused binding is an eslint failure, which is a build failure, not this test's failure).

Watch for: if it stays green, do not trust the check — two things make it vacuous. The two `GET` fixtures must **differ**; identical bodies are structurally shared, `data` keeps its reference and the effect never re-runs. And the wait must be on the cache holding `version: 3`, not on a request counter, which resolves one commit before the response is committed. A test asserting only that the banner appeared stays green under this mutation, which is how this bug reached a browser once already (spec, Mutation checks).

- [ ] **Step 6: Commit**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"   # make lint shells out to go
cd web && npx vitest run && cd .. && make lint
git add web/src/features/marriage/ProposeAgreementModal.tsx \
        web/src/features/marriage/ProposeAgreementModal.test.tsx \
        web/src/features/marriage/agreementCopy.ts \
        web/src/features/marriage/AgreementsPage.tsx
git commit -F- <<'EOF'
feat(agreements): the propose modal, and a latch a refetch cannot lift

One seed for all four entry points, three real radios, and the component-local
one-way hadConflict latch decision 13 needs: the hook's own state clears on the
next background refetch and would re-enable Send over wording that has moved.

Mutation: added `useEffect(() => setHadConflict(false), [data])` under the
latch's useState, with `data` taken from useAgreements().
Red: ProposeAgreementModal.test.tsx "a 409 latches Send off, and it stays off
across the refetch it triggers", on toBeDisabled() -- "expected element to be
disabled". A TEST failure, not a build failure: the suite compiled and the
other two cases stayed green. Restored both lines.
EOF
```

---

### Task 14: The New section modal, and where "Add your first agreement" goes

**Files:**
- Create: `web/src/features/marriage/NewSectionModal.tsx`, `web/src/features/marriage/NewSectionModal.test.tsx`
- Modify: `web/src/features/marriage/agreementCopy.ts` (append inside `AGREEMENT_COPY`), `web/src/features/marriage/AgreementsPage.tsx`, `web/src/features/marriage/AgreementsPage.test.tsx`

**Interfaces:**

Consumes — full signatures here, not a pointer at another task's block.

```ts
// web/src/features/marriage/useAgreements.ts (Task 9)
useAgreements(): {
  createSection: (name: string) => Promise<{ section: AgreementSection; agreements: AgreementsDocument }>
  isCreatingSection: boolean
  reload: () => Promise<void>
  // …and the rest of the hook, unused here
}
handleWriteError(err: unknown, reload: () => Promise<void>, fallback: string): string

// web/src/features/marriage/agreementCopy.ts (Task 9)
AGREEMENT_COPY   // including sectionNameTaken, useStarterSet, addFirstAgreement
                 // and the seeded-state copy, all of which ALREADY EXIST

// web/src/features/marriage/ProposeAgreementModal.tsx (Task 13)
type AgreementProposeSeed = { mode: "add" | "edit" | "remove"; sectionId?: string
                              targetAgreementId?: string; body?: string }

// web/src/components/Modal.tsx, web/src/api/client.ts
Modal, ApiError

// web/src/features/marriage/AgreementsPage.test.tsx (Task 10's shared helpers)
renderPage(routes: Record<string, RouteResponse | RouteResponse[]>): ReturnType<typeof renderWithRouter>
documentFixture(overrides?: Partial<AgreementsDocument>): AgreementsDocument
emptyDoc(o?: Partial<AgreementsDocument>): { agreements: AgreementsDocument }   // WRAPPED — two owners, no sections
seededDoc(o?: Partial<AgreementsDocument>): { agreements: AgreementsDocument }  // WRAPPED — two owners, the four starter sections, each count 0 / visible false
```

`emptyDoc()` and `seededDoc()` return the response **already wrapped** — Task 10 built them that way, and the `// WRAPPED` note above is there because wrapping them a second time is the mistake that costs an afternoon: `{agreements:{agreements:{…}}}` fails the Zod parse, `apiFetch` throws, and the test times out on a `findByRole` instead of naming the cause. So the stub is `body: emptyDoc()`. That is the same envelope `documentFixture` is used inside (plan header, Conventions). If Task 10's `renderPage` does not already register `GET /api/v1/auth/me`, add it to the map you pass — the page calls `useMe()` and `stubFetchRoutes` throws on an unregistered request rather than failing quietly.

Produces:

```ts
export function NewSectionModal(props: {
  onCreated: (seed: AgreementProposeSeed) => void
  onClose: () => void
}): JSX.Element
```

It hands the page a seed and the page does the close-then-open swap, because nothing here stacks `<dialog>`s.

Two things this task does **not** do: the header's **+ Section** button and the `newSectionOpen` state it sets both land in Task 10 (plan header, Conventions), as does the empty/seeded branch split and its "Use starter set" test. What is new here is the modal, its mount, and the destination of "Add your first agreement" in each of the two empty branches.

- [ ] **Step 1: Write the failing tests** — Create `web/src/features/marriage/NewSectionModal.test.tsx` (same three mechanics as Task 13: `fireEvent`, per-route `capture`, literal copy):

```tsx
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { NewSectionModal } from "./NewSectionModal";

const EMPTY = {
  locked: false,
  owners: [
    { membershipId: "m-a", name: "Andreas" },
    { membershipId: "m-c", name: "Christine" },
  ],
  version: 1,
  updatedAt: null,
  sections: [],
  proposals: [],
  history: [],
};

const NEW_SECTION = { id: "s-new", name: "Faith & values", count: 0, visible: false, agreements: [] };

function renderNewSection(routes: Record<string, RouteResponse | RouteResponse[]> = {}) {
  const posts = { count: 0 };
  const onCreated = vi.fn();
  stubFetchRoutes({
    "GET /api/v1/marriage/agreements": { status: 200, body: { agreements: EMPTY } },
    "POST /api/v1/marriage/agreements/sections": {
      status: 201,
      body: { section: NEW_SECTION, agreements: { ...EMPTY, sections: [NEW_SECTION] } },
      capture: () => {
        posts.count += 1;
      },
    },
    ...routes,
  });
  renderWithRouter(<NewSectionModal onCreated={onCreated} onClose={() => {}} />);
  return { posts, onCreated };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("NewSectionModal", () => {
  // The chips FILL the input (spec, The modals). One that submitted would make
  // a single click an unsigned write with the wrong name on it.
  it("a suggestion chip fills the input, and Create seeds an add on the new section", async () => {
    const { posts, onCreated } = renderNewSection();

    fireEvent.click(await screen.findByRole("button", { name: "Faith & values" }));
    expect(screen.getByLabelText("Section name")).toHaveValue("Faith & values");

    fireEvent.click(screen.getByRole("button", { name: "Create & add first agreement" }));
    await waitFor(() =>
      expect(onCreated).toHaveBeenCalledWith({ mode: "add", sectionId: "s-new" }),
    );

    // Exactly one POST, counted AFTER the Create has landed. Asserting `0`
    // straight after the chip click would prove nothing: mutateAsync awaits its
    // own hooks before it ever calls fetch, so the counter is still 0 on the
    // next line whatever the chip did.
    expect(posts.count).toBe(1);
  });

  // Decision 19's mapping, under the field: the modal stays open and the typed
  // name survives, or this is the empty-<select> dead end again -- a generic
  // red box four clicks in (criterion 13).
  it("a duplicate name says so under the field, keeping the modal and the name", async () => {
    renderNewSection({
      "POST /api/v1/marriage/agreements/sections": {
        status: 409,
        body: {
          error: { code: "AGREEMENT_SECTION_NAME_TAKEN", message: "That section already exists." },
        },
      },
    });

    fireEvent.change(await screen.findByLabelText("Section name"), {
      target: { value: "Money" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create & add first agreement" }));

    // Substring, deliberately: the sentence is asserted, its trailing full stop
    // belongs to Task 9's own copy value and is not this test's to pin.
    expect(await screen.findByTestId("agreement-section-name-error")).toHaveTextContent(
      "You already have a section called that",
    );
    expect(screen.getByLabelText("Section name")).toHaveValue("Money");
    expect(screen.getByRole("heading", { name: "New agreement section" })).toBeInTheDocument();
  });
});
```

Then add to `AgreementsPage.test.tsx`, reusing Task 10's helpers by name. These two cases are the ones Task 10 did not write — where "Add your first agreement" **goes** in each empty branch. Do not re-add Task 10's "Use starter set lands on the seeded state" test; it is already there.

```tsx
// State 3 (no sections): this button opens NEW SECTION, not Propose -- the
// propose picker's section select would be empty, which is the BillsPage dead
// end docs/LEARNING.md records, on the first screen anyone sees.
it("with no sections, Add your first agreement opens the New section modal", async () => {
  renderPage({
    "GET /api/v1/marriage/agreements": { status: 200, body: emptyDoc() },
  });

  fireEvent.click(await screen.findByRole("button", { name: "Add your first agreement" }));

  // By heading role: "New agreement section" is the Modal's own <h2>, and the
  // page has no other element carrying that text.
  expect(
    await screen.findByRole("heading", { name: "New agreement section" }),
  ).toBeInTheDocument();
});

// State 4 (sections seeded, nothing agreed): the same button opens PROPOSE, on
// the first section, because now there is something for the picker to offer.
it("with sections seeded, Add your first agreement opens Propose on the first section", async () => {
  const seeded = seededDoc();
  renderPage({
    "GET /api/v1/marriage/agreements": { status: 200, body: seeded },
  });

  fireEvent.click(await screen.findByRole("button", { name: "Add your first agreement" }));

  // The heading again, not getByText: the header's own "Propose a change"
  // button carries the same string, and getByText would find two elements.
  expect(await screen.findByRole("heading", { name: "Propose a change" })).toBeInTheDocument();
  expect(screen.getByRole("radio", { name: "Add new" })).toBeChecked();
  expect(screen.getByLabelText("Add to section")).toHaveValue(seeded.agreements.sections[0].id);
});
```

- [ ] **Step 2: Run them and watch them fail** — `cd web && npx vitest run src/features/marriage/NewSectionModal.test.tsx src/features/marriage/AgreementsPage.test.tsx`

Expected, and they are two different kinds of failure — say which you saw where:

- `NewSectionModal.test.tsx`: a **collection (build) failure** — `Error: Failed to resolve import "./NewSectionModal" from "src/features/marriage/NewSectionModal.test.tsx". Does the file exist?`
- `AgreementsPage.test.tsx`: two real **test failures**. Task 10 rendered "Add your first agreement" with no handler, so the click does nothing and each case times out on its `findByRole`:
  `TestingLibraryElementError: Unable to find an accessible element with the role "heading" and name "New agreement section"`, and the same for `"Propose a change"`.

- [ ] **Step 3: Build the modal and wire the two destinations** — First append this task's copy **inside `AGREEMENT_COPY`**. Before typing, check what is already there:

```
grep -n "sectionNameTaken\|starterSet\|addFirstAgreement\|sectionsReady\|seeded\|cancel:" \
  web/src/features/marriage/agreementCopy.ts
```

`sectionNameTaken`, `useStarterSet`, `addFirstAgreement` and the seeded-state headline and body **already exist in `AGREEMENT_COPY`** from Task 9. Task 10 rendered "Use starter set" and both empty panels but deliberately left "Add your first agreement" out, because it opens a modal and every modal-opening control lands with its modal — so **this task adds that button**, in both panels. Reuse those keys; a second key for the same string is at best noise and at worst a duplicate key, which is a TypeScript error. Only the modal's own strings are new:

```ts
  // --- New section modal (Task 14) ---
  newSectionTitle: "New agreement section",
  newSectionSubtitle: "Sections just group related agreements — add your own beyond the starters",
  sectionNameLabel: "Section name",
  sectionNamePlaceholder: "e.g. In-laws, Faith, Health, Careers",
  suggestionsLabel: "Or pick a suggestion",
  // Suggestions, not the starter set: unrelated to domain.StarterSectionNames()'
  // four (decision 17), which "Use starter set" seeds and this list does not.
  sectionSuggestions: ["In-laws & family", "Faith & values", "Health", "Careers", "Screens & tech"],
  newSectionCreate: "Create & add first agreement",
  newSectionFallbackError: "Could not create that section. Try again.",
```

Then create `web/src/features/marriage/NewSectionModal.tsx`:

```tsx
// A section name, five suggestion chips, and one button that creates the
// section and takes you straight into Propose on it.
//
// Creating a section is immediate and unsigned (decision 8): a heading is not a
// promise, so nothing here proposes anything. The agreement that follows is an
// ordinary proposal, which is why this hands the page a seed rather than
// writing one itself.
import { useId, useState } from "react";
import { Modal } from "../../components/Modal";
import { ApiError } from "../../api/client";
import { AGREEMENT_COPY } from "./agreementCopy";
import { handleWriteError, useAgreements } from "./useAgreements";
import type { AgreementProposeSeed } from "./ProposeAgreementModal";

export function NewSectionModal({
  onCreated,
  onClose,
}: {
  // Handed the seed for the section just created. The page closes this modal
  // and opens Propose with it -- nothing here stacks <dialog>s.
  onCreated: (seed: AgreementProposeSeed) => void;
  onClose: () => void;
}) {
  const { createSection, isCreatingSection, reload } = useAgreements();
  const nameFieldId = useId();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);

  async function handleCreate() {
    setError(null);
    try {
      // createSection is the one write that returns more than the document,
      // precisely so this line has an id to seed Propose with.
      const { section } = await createSection(name.trim());
      onCreated({ mode: "add", sectionId: section.id });
    } catch (err) {
      // The one refusal this field can name (decision 19). It keeps the modal
      // open with the typed name intact -- a generic red box four clicks in is
      // the dead end criterion 13 exists to catch -- and deliberately does NOT
      // reload: nothing about the document changed, and a refetch here would
      // make a name clash look like the screen resetting under the person.
      if (err instanceof ApiError && err.code === "AGREEMENT_SECTION_NAME_TAKEN") {
        setError(AGREEMENT_COPY.sectionNameTaken);
        return;
      }
      setError(handleWriteError(err, reload, AGREEMENT_COPY.newSectionFallbackError));
    }
  }

  return (
    <Modal open onClose={onClose} title={AGREEMENT_COPY.newSectionTitle}>
      <p className="-mt-2 mb-4 text-xs text-muted">{AGREEMENT_COPY.newSectionSubtitle}</p>

      <form
        className="flex flex-col gap-4"
        onSubmit={(event) => {
          // Same guard as the propose modal's: a single-input form implicitly
          // submits on Enter, and both footer buttons are type="button".
          event.preventDefault();
        }}
      >
        <div className="flex flex-col gap-1.5">
          <label htmlFor={nameFieldId} className="text-xs font-semibold text-label">
            {AGREEMENT_COPY.sectionNameLabel}
          </label>
          <input
            id={nameFieldId}
            type="text"
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder={AGREEMENT_COPY.sectionNamePlaceholder}
            // MaxAgreementSectionNameLen, as a courtesy; the server counts runes.
            maxLength={60}
            className="min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0"
          />
          {/* Directly under the input, never a footer banner: the refusal is
              about this field, and criterion 13 is that the person can see
              which one. */}
          {error !== null && (
            <p
              data-testid="agreement-section-name-error"
              role="alert"
              className="text-xs leading-snug text-danger"
            >
              {error}
            </p>
          )}
        </div>

        <div className="flex flex-col gap-2">
          <span className="text-xs font-semibold text-label">
            {AGREEMENT_COPY.suggestionsLabel}
          </span>
          <div className="flex flex-wrap gap-2">
            {AGREEMENT_COPY.sectionSuggestions.map((suggestion) => (
              // Fills the input; does not submit. A chip that created the
              // section would turn one click into a write nobody got to
              // rename, and the design draws these as suggestions under the
              // field rather than as choices that act.
              <button
                key={suggestion}
                type="button"
                onClick={() => setName(suggestion)}
                className="min-h-11 rounded-full border border-hairline px-3.5 text-[12.5px] text-label sm:min-h-0 sm:py-1.5"
              >
                {suggestion}
              </button>
            ))}
          </div>
        </div>

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={onClose}
            className="min-h-11 flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label sm:min-h-0"
          >
            {AGREEMENT_COPY.cancel}
          </button>
          <button
            type="button"
            disabled={isCreatingSection || name.trim() === ""}
            onClick={() => void handleCreate()}
            className="min-h-11 flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
          >
            {AGREEMENT_COPY.newSectionCreate}
          </button>
        </div>
      </form>
    </Modal>
  );
}
```

Then two edits in `AgreementsPage.tsx`.

**The mount**, beside Task 13's, as the last children of the page's outermost element:

```tsx
{newSectionOpen && (
  <NewSectionModal
    onClose={() => setNewSectionOpen(false)}
    onCreated={(seed) => {
      // Close, then open. Both booleans are Task 10's; this is the swap.
      setNewSectionOpen(false);
      setProposeSeed(seed);
    }}
  />
)}
```

**The button, and its two destinations.** `grep -n "addFirstAgreement" web/src/features/marriage/AgreementsPage.tsx` returns nothing before this step: Task 10 left the control out on purpose (its comment beside "Use starter set" says so). Add it to **both** empty panels, above the "Use starter set" button in state 3 — the design's own order is "Add your first agreement", then "Use starter set" — and as the only call to action in state 4, where "Use starter set" is gone. It is the same button either way and differs only in what it opens. Give each its own:

```tsx
// State 3, doc.sections.length === 0 (data-testid="agreements-empty"), placed
// immediately BEFORE the existing "Use starter set" button:
<button
  type="button"
  className="min-h-11 rounded-lg bg-accent px-5 text-[13px] font-semibold text-white"
  onClick={() => setNewSectionOpen(true)}
>
  {AGREEMENT_COPY.addFirstAgreement}
</button>

// State 4, sections seeded but nothing agreed (data-testid="agreements-seeded"),
// where it is the only call to action -- "Use starter set" is gone by then:
<button
  type="button"
  className="min-h-11 rounded-lg bg-accent px-5 text-[13px] font-semibold text-white"
  onClick={() => setProposeSeed({ mode: "add", sectionId: doc.sections[0].id })}
>
  {AGREEMENT_COPY.addFirstAgreement}
</button>
```

Both carry `min-h-11`, the 44px touch-target floor, and both read their label from
`AGREEMENT_COPY.addFirstAgreement` rather than a literal — one copy object, so the two
buttons cannot drift into two spellings of the same words.

The split itself is Task 10's and stays as it is. The reason the two differ is the whole point of the split: with no sections, Propose's section select would be empty, and an empty select that still offers a submit button is the `BillsPage` dead end `docs/LEARNING.md` records — on the first screen anyone sees.

- [ ] **Step 4: Run the tests and watch them pass** — `cd web && npx vitest run src/features/marriage`

Expected: PASS across the folder. Both new page tests mount a second modal on top of the page and neither needs a new route: every modal here reads through `useAgreements()`, which shares the page's one query key.

- [ ] **Step 5: Mutation-check the chip and the surviving name** — Two, both named.

**First**, make a suggestion chip submit as well as fill — `onClick={() => { setName(suggestion); void handleCreate(); }}`. Expected: **"a suggestion chip fills the input, and Create seeds an add on the new section"** goes red on the last line:

```
AssertionError: expected 2 to be 1
```

Not on `toHaveValue` (the chip still fills the field) and not on `onCreated` (it is called twice with the same argument, which `toHaveBeenCalledWith` accepts) — the POST counter is the only assertion that can see it, which is why it sits at the end of the test rather than after the click.

**Second**, add `setName("")` to the duplicate branch of the catch, above `setError`. Expected: **"a duplicate name says so under the field, keeping the modal and the name"** goes red on `toHaveValue("Money")`, received `""`. Both `setError` and `setName` land in one commit, so `findByTestId` already awaited the render this asserts against.

Restore both. Record in the commit body which failure text you saw for each — an orphaned import gives a build failure, which prints the same word as a test failure.

- [ ] **Step 6: Commit**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
cd web && npx vitest run && cd .. && make lint
git add web/src/features/marriage/NewSectionModal.tsx \
        web/src/features/marriage/NewSectionModal.test.tsx \
        web/src/features/marriage/agreementCopy.ts \
        web/src/features/marriage/AgreementsPage.tsx \
        web/src/features/marriage/AgreementsPage.test.tsx
git commit -F- <<'EOF'
feat(agreements): new sections, chips that fill rather than submit, and where
"Add your first agreement" goes

Creating a section is immediate and unsigned (decision 8); the agreement that
follows is an ordinary proposal, so the modal hands the page a seed rather than
writing one. A duplicate name is named under the field (decision 19), and the
empty state's button opens New section, not Propose, because Propose's picker
would be empty there.

Mutation 1: chip onClick changed to `setName(s); void handleCreate();`.
Red: NewSectionModal.test.tsx "a suggestion chip fills the input, and Create
seeds an add on the new section", on the POST counter -- "expected 2 to be 1".
A TEST failure.
Mutation 2: `setName("")` added to the AGREEMENT_SECTION_NAME_TAKEN branch.
Red: NewSectionModal.test.tsx "a duplicate name says so under the field,
keeping the modal and the name", on toHaveValue("Money") -- received "".
A TEST failure. Both restored.
EOF
```

---

### Task 15: Version history and Restore

**Files:**
- Create: `web/src/features/marriage/VersionHistoryModal.tsx`, `web/src/features/marriage/VersionHistoryModal.test.tsx`
- Modify: `web/src/features/marriage/agreementCopy.ts` (append inside `AGREEMENT_COPY`), `web/src/features/marriage/AgreementsPage.tsx`, `web/src/features/marriage/AgreementsPage.test.tsx`

**Interfaces:**

Consumes — full signatures here, not a pointer at another task's block.

```ts
// web/src/features/marriage/useAgreements.ts (Task 9)
useAgreements(): {
  data: AgreementsDocument | undefined
  isLoading: boolean
  // …and the rest of the hook. This file uses NO mutation and makes no second
  // fetch: history travels on the document (decision 9; spec, The modals).
}

// web/src/features/marriage/agreementSchemas.ts (Task 9)
type AgreementHistoryEntry = AgreementsDocument["history"][number]
//   = { version: number; proposalId: string; kind: "add" | "edit" | "remove"
//       sectionId: string; sectionName: string; body: string; previousBody: string
//       note: string; proposedByName: string; signedByNames: string[]; acceptedAt: string }

// web/src/features/marriage/agreementCopy.ts (Task 9)
AGREEMENT_COPY
historyDateLabel(iso: string): string   // "28 Jun 2026" -- en-GB, { day: "numeric",
                                        // month: "short", year: "numeric" }. History
                                        // spans years by construction, so this one
                                        // carries its year where agreementDateLabel
                                        // does not (plan header, Conventions)
joinNames(names: string[]): string      // joins with " and "; used by historySignedBy
                                        // below, inside that same module

// web/src/features/marriage/ProposeAgreementModal.tsx (Task 13)
type AgreementProposeSeed

// web/src/components/Modal.tsx
Modal

// web/src/features/marriage/AgreementsPage.test.tsx (Task 10's shared helpers)
renderPage(routes: Record<string, RouteResponse | RouteResponse[]>): ReturnType<typeof renderWithRouter>
documentFixture(overrides?: Partial<AgreementsDocument>): AgreementsDocument
```

Produces:

```ts
export function VersionHistoryModal(props: {
  onRestore: (seed: AgreementProposeSeed) => void
  onClose: () => void
}): JSX.Element
```

The header's **Version history** button is Task 10's, as is hiding the other two header buttons when `doc.locked` — this one stays visible in the locked-with-content state, being a read (decision 3). Nothing in this file branches on `locked`: it renders the document and offers Restore, which opens a Propose modal the locked page never mounts, because `proposeSeed` can only be set by controls Task 10 has already hidden.

- [ ] **Step 1: Write the failing tests** — Create `web/src/features/marriage/VersionHistoryModal.test.tsx`:

```tsx
import { fireEvent, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes } from "../../test/fetchStub";
import { VersionHistoryModal } from "./VersionHistoryModal";

const entry = (version: number, over: Record<string, unknown> = {}) => ({
  version,
  proposalId: `p-${version}`,
  kind: "add",
  sectionId: "s-money",
  sectionName: "Money",
  body: `Agreement ${version}`,
  previousBody: "",
  note: "",
  proposedByName: "Andreas",
  signedByNames: ["Andreas", "Christine"],
  acceptedAt: "2026-06-28T09:00:00Z",
  ...over,
});

function renderHistory(history: unknown[], version: number) {
  const onRestore = vi.fn();
  stubFetchRoutes({
    "GET /api/v1/marriage/agreements": {
      status: 200,
      body: {
        agreements: {
          locked: false,
          owners: [
            { membershipId: "m-a", name: "Andreas" },
            { membershipId: "m-c", name: "Christine" },
          ],
          version,
          updatedAt: "2026-06-28T09:00:00Z",
          sections: [],
          proposals: [],
          history,
        },
      },
    },
  });
  renderWithRouter(<VersionHistoryModal onRestore={onRestore} onClose={() => {}} />);
  return onRestore;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("VersionHistoryModal", () => {
  // Deliberately mis-ordered: nothing in the schema enforces the server's
  // newest-first ordering, so a document whose newest change is not history[0]
  // must still crown the right row (spec, "…its disclosure").
  it("marks current the row whose version equals the document's, not the first", async () => {
    renderHistory([entry(3), entry(4), entry(2)], 4);

    expect(await screen.findByText("v4 · current")).toBeInTheDocument();
    expect(screen.getByText("v3")).toBeInTheDocument();
  });

  // Four render, two collapse, and BOTH bounds come from the ones that
  // collapsed. There is no v1 row: a document is v1 before anything is agreed,
  // so the first accepted proposal produces v2 (decision 10) and a hardcoded 1
  // is wrong by construction.
  it("collapses all but the four newest, bounded by what actually collapsed", async () => {
    renderHistory([entry(7), entry(6), entry(5), entry(4), entry(3), entry(2)], 7);

    expect(await screen.findByRole("button", { name: "Show v2–v3 ↓" })).toBeInTheDocument();
    expect(screen.queryByText("v3")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Show v2–v3 ↓" }));
    expect(screen.getByText("v3")).toBeInTheDocument();
  });

  // The sentence is derived from `kind` with no guessing default, and Restore
  // is offered on removals only (decision 18) -- an edit back is one click away
  // through the ordinary flow.
  it("derives the sentence from the kind, and offers Restore on removals only", async () => {
    const onRestore = renderHistory(
      [
        entry(4, { kind: "remove", body: "", previousBody: "No phones at dinner" }),
        entry(3, { kind: "edit", previousBody: "Old wording" }),
        entry(2),
      ],
      4,
    );

    const row = await screen.findByTestId("agreement-history-p-4");
    expect(row).toHaveTextContent('Removed from Money: "No phones at dinner"');
    // joinNames joins with " and ", not the design's "&" (plan header,
    // Conventions) -- one join, one assertion, everywhere.
    expect(row).toHaveTextContent("Agreed by Andreas and Christine");
    expect(screen.getByTestId("agreement-history-p-3")).toHaveTextContent(
      'Changed in Money: "Agreement 3"',
    );

    const restoreButtons = screen.getAllByRole("button", { name: "Restore" });
    expect(restoreButtons).toHaveLength(1);
    fireEvent.click(restoreButtons[0]);
    expect(onRestore).toHaveBeenCalledWith({
      mode: "add",
      body: "No phones at dinner",
      sectionId: "s-money",
    });
  });
});
```

Then one case in `AgreementsPage.test.tsx`, because nothing else proves the header button reaches this modal — Goals shipped archive-and-restore with no screen calling it, which is the class of defect the spec's "grep every mutation for a caller outside it" exists for:

```tsx
it("Version history opens the modal off the document the page already has", async () => {
  renderPage({
    "GET /api/v1/marriage/agreements": { status: 200, body: { agreements: documentFixture() } },
  });

  fireEvent.click(await screen.findByRole("button", { name: "Version history" }));

  // The Modal's own <h2>. If stubFetchRoutes throws "no stub registered" here,
  // the modal is fetching under a key of its own -- which is the defect, not
  // the stub's.
  expect(await screen.findByRole("heading", { name: "Version history" })).toBeInTheDocument();
});
```

- [ ] **Step 2: Run them and watch them fail** — `cd web && npx vitest run src/features/marriage/VersionHistoryModal.test.tsx src/features/marriage/AgreementsPage.test.tsx`

Expected, two different kinds — say which you saw where:

- `VersionHistoryModal.test.tsx`: a **collection (build) failure** — `Error: Failed to resolve import "./VersionHistoryModal" from "src/features/marriage/VersionHistoryModal.test.tsx". Does the file exist?`
- `AgreementsPage.test.tsx`: one real **test failure**. Task 10's button sets `historyOpen` and nothing renders off it yet, so:
  `TestingLibraryElementError: Unable to find an accessible element with the role "heading" and name "Version history"`.
  Note that the *button* of that name is on screen — this is exactly why the assertion asks for the heading role.

- [ ] **Step 3: Build the modal** — Append this task's copy **inside `AGREEMENT_COPY`**. Check first:

```
grep -n "loading\|historyEmpty" web/src/features/marriage/agreementCopy.ts
```

Task 9 already defined `loading: "Loading…"`, so this modal reuses it: there is no `loadingHistory`, and adding one would put the same string under two keys.

```ts
  // --- Version history modal (Task 15) ---
  historyTitle: "Version history",
  historySubtitle: "Every change, and who agreed",
  historyRestore: "Restore",
  historyVersion: (version: number) => `v${version}`,
  historyVersionCurrent: (version: number) => `v${version} · current`,
  historyEmpty: "Nothing has been agreed yet, so there is no history.",
  // The design's "Added #12 …" carries a display number. A change accepted two
  // years ago has no position in today's document (decision 11), so the section
  // name takes its place -- which is also why the wire carries no number here.
  // No default arm: the Zod enum is the fail-closed boundary, and a fourth kind
  // needs a migration before it can reach this switch.
  historySentence: (kind: "add" | "edit" | "remove", sectionName: string, text: string) => {
    switch (kind) {
      case "add":
        return `Added to ${sectionName}: "${text}"`;
      case "edit":
        return `Changed in ${sectionName}: "${text}"`;
      case "remove":
        return `Removed from ${sectionName}: "${text}"`;
    }
  },
  // A signer whose membership no longer resolves is omitted server-side rather
  // than joined as "", or this reads "Agreed by Andreas and ".
  historySignedBy: (signedByNames: string[]) => `Agreed by ${joinNames(signedByNames)}`,
  historyShowOlder: (lowest: number, highest: number) =>
    lowest === highest ? `Show v${lowest} ↓` : `Show v${lowest}–v${highest} ↓`,
```

Then create `web/src/features/marriage/VersionHistoryModal.tsx`:

```tsx
// Every accepted change, newest first, with Restore on the removals.
//
// It renders `data.history` off the document the page already holds and
// FETCHES NOTHING OF ITS OWN. History travels on the document because it is
// composed from rows the read already walked (decision 9), so a second
// endpoint would refetch what the first threw away and go stale after every
// Agree -- and opening this modal can never show a version the page below it
// disagrees with, because there is only one version to disagree about.
import { useState } from "react";
import { Modal } from "../../components/Modal";
import { AGREEMENT_COPY, historyDateLabel } from "./agreementCopy";
import { useAgreements } from "./useAgreements";
import type { AgreementHistoryEntry } from "./agreementSchemas";
import type { AgreementProposeSeed } from "./ProposeAgreementModal";

// How many rows render before the rest collapse (spec, "…its disclosure").
const NEWEST = 4;

export function VersionHistoryModal({
  onRestore,
  onClose,
}: {
  // Handed a seed for an ordinary add proposal (decision 18). The page closes
  // this modal and opens Propose with it: nothing here stacks <dialog>s, and
  // Task 13's latch wants a fresh mount anyway.
  onRestore: (seed: AgreementProposeSeed) => void;
  onClose: () => void;
}) {
  const { data, isLoading } = useAgreements();
  const [expanded, setExpanded] = useState(false);

  const history = data?.history ?? [];
  const shown = expanded ? history : history.slice(0, NEWEST);
  const collapsed = history.slice(NEWEST);
  // Both bounds off the entries that actually collapsed, the way retroCopy.ts's
  // showOlderYear takes both from data. A hardcoded 1 is wrong by
  // construction: there is no v1 row, because v1 is the document before
  // anything was agreed (decision 10).
  const lowest = collapsed.length > 0 ? Math.min(...collapsed.map((e) => e.version)) : 0;
  const highest = collapsed.length > 0 ? Math.max(...collapsed.map((e) => e.version)) : 0;

  // By VALUE, never by array position: nothing in the schema enforces the
  // server's newest-first ordering, and a document whose newest change is not
  // history[0] would crown the wrong row.
  const isCurrent = (entry: AgreementHistoryEntry) => entry.version === data?.version;

  return (
    <Modal open onClose={onClose} title={AGREEMENT_COPY.historyTitle}>
      <p className="-mt-2 mb-4 text-xs text-muted">{AGREEMENT_COPY.historySubtitle}</p>

      {data === undefined ? (
        // On the real screen this branch is a formality: the page above has
        // already answered this query and this modal reads the same key from
        // the cache. It exists because a component may not assume a warm
        // cache, and rendering historyEmpty while the answer is still in
        // flight would tell a household with years of history that it has
        // none -- the vacuous-first-render defect the spec names for `locked`.
        <p className="text-xs text-muted">
          {isLoading ? AGREEMENT_COPY.loading : AGREEMENT_COPY.historyEmpty}
        </p>
      ) : (
        // The content block scrolls, not the panel -- VisionModal.tsx:665's
        // exact shape. Do NOT reach for the design's `hb-scroll` class; it does
        // not exist in web/src.
        <div className="flex max-h-[65vh] flex-col gap-1 overflow-y-auto pr-1">
          {history.length === 0 ? (
            <p className="text-xs text-muted">{AGREEMENT_COPY.historyEmpty}</p>
          ) : (
            <ul className="flex flex-col gap-1">
              {shown.map((entry) => (
                <li
                  key={entry.proposalId}
                  data-testid={`agreement-history-${entry.proposalId}`}
                  className="rounded-[10px] border border-hairline p-3"
                >
                  <div className="flex items-center gap-2">
                    <span
                      aria-hidden="true"
                      className={`h-2 w-2 flex-none rounded-full ${
                        isCurrent(entry) ? "bg-accent" : "bg-hairline"
                      }`}
                    />
                    <span className="text-[12.5px] font-semibold text-ink">
                      {isCurrent(entry)
                        ? AGREEMENT_COPY.historyVersionCurrent(entry.version)
                        : AGREEMENT_COPY.historyVersion(entry.version)}
                    </span>
                    <span className="ml-auto text-[11.5px] text-muted">
                      {historyDateLabel(entry.acceptedAt)}
                    </span>
                  </div>

                  {/* A removal's own wording is `previousBody` -- its `body` is
                      empty by constraint -- and that is the copy the design
                      promises stays readable here. */}
                  <p className="mt-1.5 text-[13px] leading-relaxed text-ink">
                    {AGREEMENT_COPY.historySentence(
                      entry.kind,
                      entry.sectionName,
                      entry.kind === "remove" ? entry.previousBody : entry.body,
                    )}
                  </p>

                  <div className="mt-1.5 flex items-center justify-between gap-3">
                    <span className="text-[11.5px] text-muted">
                      {AGREEMENT_COPY.historySignedBy(entry.signedByNames)}
                    </span>
                    {entry.kind === "remove" && (
                      // Removals only. An edit entry gets no Restore: that is
                      // an edit back, one click away through the ordinary flow.
                      // The section always still exists (sections are never
                      // removed) and may be empty and invisible, which the
                      // propose picker still offers.
                      <button
                        type="button"
                        onClick={() =>
                          onRestore({
                            mode: "add",
                            body: entry.previousBody,
                            sectionId: entry.sectionId,
                          })
                        }
                        className="min-h-11 flex-none rounded-lg border border-hairline px-3 text-[12px] font-semibold text-accent sm:min-h-0 sm:py-1.5"
                      >
                        {AGREEMENT_COPY.historyRestore}
                      </button>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}

          {collapsed.length > 0 && !expanded && (
            // A real button, and the reveal fetches nothing: everything it
            // shows is already in `history`.
            <button
              type="button"
              onClick={() => setExpanded(true)}
              className="min-h-11 self-start text-[12.5px] font-semibold text-accent sm:min-h-0"
            >
              {AGREEMENT_COPY.historyShowOlder(lowest, highest)}
            </button>
          )}
        </div>
      )}
    </Modal>
  );
}
```

Then mount it from `AgreementsPage.tsx`, beside the other two, using Task 10's `historyOpen`:

```tsx
{historyOpen && (
  <VersionHistoryModal
    onClose={() => setHistoryOpen(false)}
    onRestore={(seed) => {
      setHistoryOpen(false);
      setProposeSeed(seed);
    }}
  />
)}
```

Nothing here is conditioned on `doc.locked`: the **Version history** button stays in the locked-with-content state, being a read (decision 3), and the Restore button it exposes can only lead to a Propose modal opened from `proposeSeed`, which a locked page's hidden controls never set.

- [ ] **Step 4: Run the tests and watch them pass** — `cd web && npx vitest run src/features/marriage`

Expected: PASS across the folder.

- [ ] **Step 5: Mutation-check the by-value lookup and the bounds** — Two, both named.

**First**, replace `isCurrent` with a position test — `const isCurrent = (entry: AgreementHistoryEntry) => entry.proposalId === history[0]?.proposalId;`. Expected: **"marks current the row whose version equals the document's, not the first"** goes red on the first assertion:

```
TestingLibraryElementError: Unable to find an element with the text: v4 · current
```

because that fixture's `history[0]` is v3, which is exactly why it is mis-ordered on purpose. Only that test reddens; the other two crown their own `history[0]` anyway, which is why the mis-ordered fixture is the whole check.

**Second**, hardcode `const lowest = 1;`. Expected: **"collapses all but the four newest, bounded by what actually collapsed"** goes red on:

```
TestingLibraryElementError: Unable to find an accessible element with the role "button" and name "Show v2–v3 ↓"
```

The button is on screen reading `Show v1–v3 ↓`, which is the point: there is no v1 row to show.

Restore both, and record in the commit body which failure text you saw — an orphaned import gives a build failure, which prints the same word as a test failure.

- [ ] **Step 6: Commit**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
cd web && npx vitest run && cd .. && make lint
git add web/src/features/marriage/VersionHistoryModal.tsx \
        web/src/features/marriage/VersionHistoryModal.test.tsx \
        web/src/features/marriage/agreementCopy.ts \
        web/src/features/marriage/AgreementsPage.tsx \
        web/src/features/marriage/AgreementsPage.test.tsx
git commit -F- <<'EOF'
feat(agreements): version history off the document it already has, and restore
as an ordinary proposal

History travels on the document (decision 9), so this modal fetches nothing and
cannot disagree with the page under it. The current row is found by value, not
by array position; the disclosure takes both bounds from the rows that actually
collapsed, because there is no v1 row to fall back on. Restore is an add
proposal needing everyone's agreement (decision 18).

Mutation 1: isCurrent changed to compare against history[0].proposalId.
Red: VersionHistoryModal.test.tsx "marks current the row whose version equals
the document's, not the first" -- "Unable to find an element with the text:
v4 · current". A TEST failure.
Mutation 2: `const lowest = 1` hardcoded over the Math.min of the collapsed
entries. Red: VersionHistoryModal.test.tsx "collapses all but the four newest,
bounded by what actually collapsed" -- "Unable to find an accessible element
with the role button and name Show v2–v3 ↓"; the button rendered "Show v1–v3 ↓".
A TEST failure. Both restored.
EOF
```

---

### Task 16: "To discuss" on the Retros page, and the one key both screens read

**Files:**
- Create: `web/src/features/marriage/AgreementsToDiscuss.tsx`, `web/src/features/marriage/AgreementsToDiscuss.test.tsx`, `web/src/features/marriage/useAgreementsInvalidation.test.tsx`
- Modify: `web/src/features/marriage/agreementCopy.ts` (four keys appended to the one `AGREEMENT_COPY` object), `web/src/features/marriage/RetrosPage.tsx` (the import, and the mount after line 236 — the `)}` closing the `noRetrosYet` ternary opened on line 153), `web/src/features/marriage/RetrosPage.test.tsx` (`renderPage`'s default route map at `:104-106`, and the two direct `stubFetchRoutes` calls at `:227-229` and `:242-244`), `web/src/routes/router.test.tsx` (the two `stubFetchRoutes` calls that actually mount `RetrosPage`, at `:513` and `:594`)

**Interfaces:**

Consumes, from Task 9 (`./useAgreements`) — copy verbatim, invent nothing:

```ts
export function useAgreements(): {
  data: AgreementsDocument | undefined
  isLoading: boolean
  error: unknown
  reload: () => Promise<void>
  createSection: (name: string) => Promise<{ section: AgreementSection; agreements: AgreementsDocument }>
  seedStarterSet: () => Promise<AgreementsDocument>
  propose: (body: ProposeBody) => Promise<AgreementsDocument>
  agree: (proposalId: string) => Promise<AgreementsDocument>
  park: (proposalId: string, note: string) => Promise<AgreementsDocument>
  withdraw: (proposalId: string) => Promise<AgreementsDocument>
  isProposing: boolean
  isCreatingSection: boolean
}

export function handleWriteError(err: unknown, reload: () => Promise<void>, fallback: string): string
```

`agree(id)` POSTs `/api/v1/marriage/agreements/proposals/{id}/agree` with no body and resolves to the **document** — `agreementProposalWriteResponseSchema.parse(raw).agreements` — never to the proposal row. Every write's `onSuccess` invalidates `agreementsQueryKey()`, which is the single `["agreements"]` key this block and `AgreementsPage` both read.

Consumes, from Task 9 (`./agreementSchemas`): the types `AgreementsDocument`, `AgreementProposal`, `AgreementSection`.

Consumes, from Task 12 (`./agreementCopy`) — the block renders **the same summary the card does** (spec, "To discuss" on the Retros page: "the same summary, the `parkNote`, and **Agree** through the same mutation"), so nothing new switches on `kind` here and there is no second fail-closed `default` to keep in step:

```ts
export function proposalSummary(
  kind: AgreementsDocument["proposals"][number]["kind"],
  proposedByName: string,
  sectionName: string,
  targetNumber: number | null,
): string
// add    -> "Andreas proposed adding to Money:"      (targetNumber unused)
// edit   -> "Andreas proposed changing Money 02:"
// remove -> "Andreas proposed removing Money 02:"
// proposedByName === "" -> "Proposed: adding to Money:" — the proposer has left
```

Consumes, from Task 9/12 (`./agreementCopy`): `AGREEMENT_COPY.agree === "Agree"`. It is Task 12's key, in the one shared object — **check before typing it, a duplicate key inside one object literal is a TypeScript error**, and if you are running out of order and it is genuinely absent, add it there rather than starting a second copy export.

Consumes, from Tasks 10 and 12 (`./AgreementsPage`): `<AgreementsPage />`, which calls `useMe()` (so every test that mounts it stubs `GET /api/v1/auth/me`) and renders one `<ProposalCard>` per `doc.proposals` entry, passing `targetNumber` derived as `doc.sections.flatMap((s) => s.agreements).find((a) => a.id === p.targetAgreementId)?.number ?? null`. Task 16 does not modify that page; it mounts it in one test, beside this block, to prove one invalidation serves both.

Produces: `<AgreementsToDiscuss />`, no props — it owns its own `useAgreements()` call the way `RetroDetail.tsx` owns its own `useRetro(month)` and `VisionCard` owns its own `useVision()`. Plus four keys appended to `AGREEMENT_COPY`.

- [ ] **Step 1: Write the failing tests**

Create `web/src/features/marriage/AgreementsToDiscuss.test.tsx`. Literal strings, never `AGREEMENT_COPY`'s exports — importing the copy module here makes every assertion tautological against a typo in that same module (`RetrosPage.test.tsx`'s own header comment).

Two things about the fixtures. `documentFixture`, `proposalFixture`, `meFixture`, `DOC_URL` and `ME_URL` come from `./agreementFixtures` (Task 10) rather than being re-typed here — this file adds only `parkedFixture`, since no earlier task needs a parked proposal. **Every body is wrapped**: the wire is `{ "agreements": {…} }` for the read and `{ "proposal": {…}, "agreements": {…} }` for a write, and `fetchAgreements` parses the envelope, so an unwrapped fixture fails inside Zod rather than in an assertion. **The fixtures are named as Task 10's** (`DOC_URL`, `documentFixture`, `proposalFixture`) and written out here rather than imported: every Vitest file in `web/src` owns its own fixtures — there is no shared fixture module in this repo — and importing another `.test.tsx` would register that file's `describe`s inside this one.

**There is deliberately no loading test:** while the query is in flight `data` is undefined, so the filter yields an empty array and the component returns null down that path too — deleting the `isLoading` guard breaks nothing any fixture could tell apart, and a test that cannot fail protects nothing (`docs/LEARNING.md` pattern 2). The nothing-parked test uses `findByTestId(...).rejects`, which only settles after its own timeout, so a block that appears late still fails it.

```tsx
// The read-only "To discuss" block, and the claim the spec makes about it that
// no single-component test can reach: this block and AgreementsPage share ONE
// query key, so one Agree here refreshes both screens off one refetch. The
// second describe below is the only place in the suite where both consumers
// are mounted at once, which is why the spec's "assert agree's two call sites
// separately" and "both mounted components re-rendering off that one
// invalidation" land here rather than in Task 9's hook test.
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { AgreementsToDiscuss } from "./AgreementsToDiscuss";
import { AgreementsPage } from "./AgreementsPage";
import type { AgreementProposal, AgreementsDocument } from "./agreementSchemas";
// Task 10's shared module, not a fourth copy of these four helpers. A local
// meFixture here drifted from Task 10's on two fields the first time this was
// written, which is the drift the shared module exists to prevent -- and
// documentFixture/proposalFixture here return the INNER document, so the
// wrapping below is still this file's job.
import { DOC_URL, ME_URL, documentFixture, meFixture, proposalFixture } from "./agreementFixtures";
const AGREE_URL = `POST ${DOC_URL}/proposals/prop-1/agree`;
const OWNERS = [
  { membershipId: "mem-1", name: "Andreas" },
  { membershipId: "mem-2", name: "Christine" },
];

// The one fixture this file adds, because no other task needs a parked
// proposal: everything else comes from ./agreementFixtures.
function parkedFixture(o: Partial<AgreementProposal> = {}): AgreementProposal {
  return proposalFixture({
    status: "parked",
    parkNote: "I want to talk about the number",
    ...o,
  });
}

function renderBlock(
  doc: AgreementsDocument,
  extra: Record<string, RouteResponse | RouteResponse[]> = {},
) {
  const fetchMock = stubFetchRoutes({
    [`GET ${DOC_URL}`]: { status: 200, body: { agreements: doc } },
    ...extra,
  });
  return { fetchMock, ...renderWithRouter(<AgreementsToDiscuss />) };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("AgreementsToDiscuss", () => {
  it("lists the parked proposal and not the pending one, offers Agree and neither Discuss nor Withdraw, and Agree posts", async () => {
    let posted = false;
    renderBlock(
      documentFixture({
        proposals: [parkedFixture(), proposalFixture({ id: "prop-2", status: "pending" })],
      }),
      {
        [AGREE_URL]: {
          status: 200,
          body: {
            proposal: proposalFixture({ status: "accepted", awaitingNames: [], canAgree: false }),
            agreements: documentFixture({ version: 2 }),
          },
          capture: () => {
            posted = true;
          },
        },
      },
    );

    const block = await screen.findByTestId("agreements-to-discuss");
    expect(within(block).getByTestId("to-discuss-row-prop-1")).toHaveTextContent(
      "Andreas proposed adding to Money:",
    );
    expect(block).toHaveTextContent("No solo spend over $200");
    expect(block).toHaveTextContent("I want to talk about the number");
    // The parked list is the whole of this block (decision 7): a pending change
    // belongs on the Agreements page, not on a page about the next retro.
    expect(within(block).queryByTestId("to-discuss-row-prop-2")).not.toBeInTheDocument();
    // A reminder, not a second editing surface.
    expect(within(block).queryByRole("button", { name: "Discuss" })).not.toBeInTheDocument();
    expect(within(block).queryByRole("button", { name: "Withdraw" })).not.toBeInTheDocument();

    fireEvent.click(within(block).getByRole("button", { name: "Agree" }));
    await waitFor(() => expect(posted).toBe(true));
  });

  it("renders nothing at all when the household has parked nothing", async () => {
    renderBlock(documentFixture({ proposals: [proposalFixture({ status: "pending" })] }));
    await expect(screen.findByTestId("agreements-to-discuss")).rejects.toThrow();
  });

  it("says one muted line when the document fails to load, never an alert", async () => {
    stubFetchRoutes({
      [`GET ${DOC_URL}`]: { status: 500, body: { error: { code: "INTERNAL", message: "boom" } } },
    });
    renderWithRouter(<AgreementsToDiscuss />);

    const line = await screen.findByTestId("agreements-to-discuss-error");
    expect(line).toHaveTextContent("Couldn't load anything parked for this retro.");
    // This block is a guest on someone else's page: silence reads as having
    // nothing to show, and a red alert claims the Retros page itself broke.
    expect(line).not.toHaveAttribute("role", "alert");
  });

  it("a locked household still sees the parked row, with no Agree button", async () => {
    renderBlock(
      documentFixture({
        locked: true,
        owners: [OWNERS[0]],
        proposals: [parkedFixture({ canAgree: false, canWithdraw: false })],
      }),
    );

    const block = await screen.findByTestId("agreements-to-discuss");
    expect(block).toHaveTextContent("No solo spend over $200");
    expect(within(block).queryByRole("button", { name: "Agree" })).not.toBeInTheDocument();
  });

  it("shows the write failure in place and keeps the row, rather than emptying the block", async () => {
    renderBlock(documentFixture({ proposals: [parkedFixture()] }), {
      [AGREE_URL]: {
        status: 500,
        body: { error: { code: "INTERNAL", message: "boom" } },
      },
    });

    const block = await screen.findByTestId("agreements-to-discuss");
    fireEvent.click(within(block).getByRole("button", { name: "Agree" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't agree that just now.");
    expect(screen.getByTestId("to-discuss-row-prop-1")).toBeInTheDocument();
  });
});

// The spec's Frontend testing paragraph asks for two things no earlier task can
// express, because until now only one consumer of the key existed: "assert
// agree's TWO call sites separately", and "both mounted components re-rendering
// off that ONE invalidation". Mounting both under one renderWithRouter gives
// them a single QueryClient, which is exactly the production arrangement.
describe("the Agreements page and the To-discuss block, sharing one query key", () => {
  const parkedDoc = documentFixture({
    sections: [{ id: "sec-1", name: "Money", count: 0, visible: false, agreements: [] }],
    proposals: [parkedFixture()],
  });
  // What the same GET answers after the Agree lands: the proposal is gone from
  // `proposals` (it is accepted, and the document carries pending and parked
  // only) and the agreement it created is live in its section, which makes the
  // section visible for the first time.
  const agreedDoc = documentFixture({
    version: 2,
    updatedAt: "2026-09-05T10:00:00+08:00",
    sections: [
      {
        id: "sec-1", name: "Money", count: 1, visible: true,
        agreements: [{ id: "agr-1", number: 1, body: "No solo spend over $200" }],
      },
    ],
    proposals: [],
  });

  function renderBoth(agreeCapture?: () => void) {
    const fetchMock = stubFetchRoutes({
      [`GET ${ME_URL}`]: { status: 200, body: meFixture() },
      [`GET ${DOC_URL}`]: [
        { status: 200, body: { agreements: parkedDoc } },
        { status: 200, body: { agreements: agreedDoc } },
      ],
      [AGREE_URL]: {
        status: 200,
        body: {
          proposal: proposalFixture({ status: "accepted", awaitingNames: [], canAgree: false }),
          agreements: agreedDoc,
        },
        ...(agreeCapture === undefined ? {} : { capture: agreeCapture }),
      },
    });
    const view = renderWithRouter(
      <>
        <AgreementsPage />
        <AgreementsToDiscuss />
      </>,
    );
    const documentGets = () =>
      fetchMock.mock.calls.filter(
        ([input, init]) => String(input) === DOC_URL && (init?.method ?? "GET") === "GET",
      ).length;
    return { ...view, fetchMock, documentGets };
  }

  it("one Agree from the block refreshes both screens off one refetch", async () => {
    const { documentGets } = renderBoth();

    const block = await screen.findByTestId("agreements-to-discuss");
    // Two consumers, one key, one request: the block does not fetch its own copy.
    expect(documentGets()).toBe(1);
    // The section is invisible while it holds nothing live (decision 8), so its
    // name appearing later is the page having re-rendered off the new document.
    expect(screen.queryByText("Money")).not.toBeInTheDocument();

    fireEvent.click(within(block).getByRole("button", { name: "Agree" }));

    // The page re-rendered: the section it could not show before is now there.
    expect(await screen.findByText("Money")).toBeInTheDocument();
    // The block re-rendered: nothing is parked any more, so it renders nothing.
    await waitFor(() =>
      expect(screen.queryByTestId("agreements-to-discuss")).not.toBeInTheDocument(),
    );
    // Two GETs, not three: the write invalidated one key and both components
    // re-rendered off the single refetch it caused.
    expect(documentGets()).toBe(2);
  });

  it("agree has two call sites, and the page's own card posts the same write", async () => {
    let posted = 0;
    const { documentGets } = renderBoth(() => {
      posted += 1;
    });

    const block = await screen.findByTestId("agreements-to-discuss");
    const agreeButtons = screen.getAllByRole("button", { name: "Agree" });
    // One on the page's ProposalCard, one in the block. If this is 1, a
    // mutation the hook returns has no caller on one of the two screens --
    // Goals shipped archive-and-restore exactly that way.
    expect(agreeButtons).toHaveLength(2);

    // Identified by NOT being inside the block, so this test does not depend on
    // Task 12's internal test ids.
    const pageAgree = agreeButtons.find((button) => !block.contains(button));
    expect(pageAgree).toBeDefined();

    fireEvent.click(pageAgree!);
    await waitFor(() => expect(posted).toBe(1));
    await waitFor(() => expect(documentGets()).toBe(2));
  });
});
```

- [ ] **Step 2: Run them and watch them fail**

```bash
cd web && npx vitest run src/features/marriage/AgreementsToDiscuss.test.tsx
```

Expected: FAIL — `Failed to resolve import "./AgreementsToDiscuss" from "src/features/marriage/AgreementsToDiscuss.test.tsx"`. That is a **build** failure, not a test failure; say which you saw in the commit body.

- [ ] **Step 3: Append this block's strings to `agreementCopy.ts`**

Four keys, inside the existing `AGREEMENT_COPY` object — this feature has **one** copy object, and later tasks append keys to it rather than starting `TO_DISCUSS_COPY`. A duplicate key in one object literal is a TypeScript error, so read the object before typing.

```ts
  // --- To discuss, on the Retros page (Task 16) ---
  toDiscussTitle: "To discuss at the next retro",
  toDiscussSubtitle: "Parked from Agreements. Agree one here once you have talked it through.",
  toDiscussLoadError: "Couldn't load anything parked for this retro.",
```

No `parkedSummary` and no second `kind` switch: `proposalSummary` (Task 12) is what this block renders, which is what "the same summary" in the spec means and what keeps one fail-closed `default` in one place.

- [ ] **Step 4: Write the component**

Create `web/src/features/marriage/AgreementsToDiscuss.tsx`:

```tsx
// The read-only "To discuss" block the Retros page mounts (decision 7). Same
// GET, same hook and same query key as the Agreements page, so one Agree here
// refreshes both screens off a single invalidation. Frontend composition only:
// nothing writes into a retro table and no proposal links to a retro row --
// the next retro usually does not exist yet, which is the whole reason there is
// no foreign key to add.
import { useState } from "react";
import { AGREEMENT_COPY, proposalSummary } from "./agreementCopy";
import { handleWriteError, useAgreements } from "./useAgreements";

export function AgreementsToDiscuss() {
  const agreements = useAgreements();
  // One in-flight slot and one error slot, not a pair per row: only one Agree
  // is ever in flight here (RetrosPage.tsx's own starting/startError pair).
  const [agreeingId, setAgreeingId] = useState<string | null>(null);
  const [agreeError, setAgreeError] = useState<string | null>(null);

  function handleAgree(id: string) {
    setAgreeingId(id);
    setAgreeError(null);
    // handleWriteError refetches on a 409 itself: this block holds no draft, so
    // the refreshed document IS the fix -- hence no hadConflict latch, which
    // belongs to the propose modal, the one surface with typing to lose.
    agreements
      .agree(id)
      .catch((err: unknown) =>
        setAgreeError(handleWriteError(err, agreements.reload, AGREEMENT_COPY.agreeError)),
      )
      .finally(() => setAgreeingId(null));
  }

  // Silence while loading: a heading with no rows claims something is parked
  // before anything says what. Muted on error, never role="alert" -- this block
  // is a guest on the Retros page and must not look like the page broke.
  if (agreements.isLoading) return null;
  if (agreements.error !== null && agreements.error !== undefined) {
    return (
      <p data-testid="agreements-to-discuss-error" className="text-xs text-muted">
        {AGREEMENT_COPY.toDiscussLoadError}
      </p>
    );
  }

  const doc = agreements.data;
  const parked = doc?.proposals.filter((p) => p.status === "parked") ?? [];
  if (parked.length === 0) return null;

  // The target's display number as the document numbers it right now, null when
  // no live agreement carries that id -- the same derivation AgreementsPage
  // makes for its own cards. The wire carries no number on a proposal: numbering
  // is the document's and derived at render (decision 11).
  const numberOf = (targetAgreementId: string): number | null =>
    doc?.sections.flatMap((s) => s.agreements).find((a) => a.id === targetAgreementId)?.number ??
    null;

  return (
    <section
      data-testid="agreements-to-discuss"
      className="rounded-xl border border-hairline bg-card p-[22px]"
    >
      <h2 className="text-sm font-semibold text-ink">{AGREEMENT_COPY.toDiscussTitle}</h2>
      <p className="mt-1 text-xs text-muted">{AGREEMENT_COPY.toDiscussSubtitle}</p>
      {agreeError && (
        <p role="alert" className="mt-2 text-xs text-danger">
          {agreeError}
        </p>
      )}
      <ul className="mt-4 flex flex-col gap-3">
        {parked.map((p) => (
          <li
            key={p.id}
            data-testid={`to-discuss-row-${p.id}`}
            className="flex flex-wrap items-start justify-between gap-3 border-t border-hairline pt-3 first:border-t-0 first:pt-0"
          >
            <div className="min-w-0">
              <p className="text-[13px] text-ink">
                {proposalSummary(p.kind, p.proposedByName, p.sectionName, numberOf(p.targetAgreementId))}
              </p>
              {/* An edit shows what it would replace; an add has no previousBody
                  and a remove has no body, so this reads the fields rather than
                  asking the kind a second time. */}
              {p.previousBody !== "" && (
                <p className="mt-0.5 text-[12.5px] text-muted line-through">{p.previousBody}</p>
              )}
              {p.body !== "" && <p className="mt-0.5 text-[12.5px] text-ink">{p.body}</p>}
              {/* An empty park note is ordinary: Discuss is a bare button. */}
              {p.parkNote !== "" && <p className="mt-1 text-[12.5px] text-muted">{p.parkNote}</p>}
            </div>
            {/* canAgree is the server's own flag (!locked && …), so a locked
                household loses this button with no second rule in the browser to
                disagree with it (decisions 3 and 16). No Discuss and no
                Withdraw: a reminder, not a second editing surface. min-h-11 is
                the 44px touch floor. */}
            {p.canAgree && (
              <button
                type="button"
                onClick={() => handleAgree(p.id)}
                disabled={agreeingId === p.id}
                className="min-h-11 rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60 sm:min-h-0"
              >
                {AGREEMENT_COPY.agree}
              </button>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
```

- [ ] **Step 5: Run the tests and watch them pass**

```bash
cd web && npx vitest run src/features/marriage/AgreementsToDiscuss.test.tsx
```

Expected: PASS, 7 tests (five in the first describe, two in the second).

If the second describe fails on `stubFetchRoutes: no stub registered for "GET /api/v1/household/members"` or any other route, `AgreementsPage` fetches something Task 10's brief did not name — register it in `renderBoth` and say so in the commit body rather than deleting the test: this is the only place the two consumers meet.

- [ ] **Step 6: Pin every write's invalidation, not only Agree's**

Create `web/src/features/marriage/useAgreementsInvalidation.test.tsx`. The spec asks for "**every** write invalidating the agreements key"; Task 9's own test proves it for `agree` alone, and a `useMutation` written without `onSuccess: afterWrite` fails no test that only exercises its sibling. Six rows, one per write.

This file pins behaviour Task 9 already implemented, so it has **no implementation step of its own** — it must pass on its first run. Its red state comes from Step 11's mutation. If a row fails now, the hook really is missing that invalidation and fixing `useAgreements.ts` is this task's work, not a later one's.

```ts
// One row per write the hook returns. The claim is identical for all six and
// worth stating once: a write does not trust the document it got back -- it
// invalidates the one key, and the refetch is what both screens render. A
// missing onSuccess leaves a screen showing a document one write out of date,
// which is invisible to any test that asserts only the write's own return value.
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { stubFetchRoutes } from "../../test/fetchStub";
import { useAgreements } from "./useAgreements";
import type { AgreementsDocument } from "./agreementSchemas";

const DOC_URL = "/api/v1/marriage/agreements";
const OWNERS = [
  { membershipId: "mem-1", name: "Andreas" },
  { membershipId: "mem-2", name: "Christine" },
];

function documentFixture(version: number): AgreementsDocument {
  return {
    locked: false, owners: OWNERS, version, updatedAt: null,
    sections: [], proposals: [], history: [],
  };
}

const SECTION = { id: "sec-1", name: "Money", count: 0, visible: false, agreements: [] };
const PROPOSAL = {
  id: "prop-1", kind: "add" as const, status: "accepted" as const, sectionId: "sec-1",
  sectionName: "Money", targetAgreementId: "", body: "No solo spend over $200", previousBody: "",
  note: "", parkNote: "", proposedByMembershipId: "mem-1", proposedByName: "Andreas",
  proposedAt: "2026-09-05T09:00:00+08:00", awaitingNames: [], targetChanged: false,
  canAgree: false, canWithdraw: false,
};

function renderUseAgreements() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderHook(() => useAgreements(), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    ),
  });
}

afterEach(() => {
  vi.unstubAllGlobals();
});

type Write = {
  name: string;
  route: string;
  status: number;
  body: unknown;
  call: (hook: ReturnType<typeof useAgreements>) => Promise<unknown>;
};

const writes: Write[] = [
  {
    name: "createSection",
    route: `POST ${DOC_URL}/sections`,
    status: 201,
    body: { section: SECTION, agreements: documentFixture(2) },
    call: (h) => h.createSection("Money"),
  },
  {
    name: "seedStarterSet",
    route: `POST ${DOC_URL}/starter-set`,
    status: 200,
    body: { agreements: documentFixture(2) },
    call: (h) => h.seedStarterSet(),
  },
  {
    name: "propose",
    route: `POST ${DOC_URL}/proposals`,
    status: 201,
    body: { proposal: PROPOSAL, agreements: documentFixture(2) },
    call: (h) =>
      h.propose({
        kind: "add", sectionId: "sec-1", targetAgreementId: "",
        body: "No solo spend over $200", previousBody: "", note: "",
      }),
  },
  {
    name: "agree",
    route: `POST ${DOC_URL}/proposals/prop-1/agree`,
    status: 200,
    body: { proposal: PROPOSAL, agreements: documentFixture(2) },
    call: (h) => h.agree("prop-1"),
  },
  {
    name: "park",
    route: `POST ${DOC_URL}/proposals/prop-1/park`,
    status: 200,
    body: { proposal: { ...PROPOSAL, status: "parked" as const }, agreements: documentFixture(2) },
    call: (h) => h.park("prop-1", "Let us talk about the number"),
  },
  {
    name: "withdraw",
    route: `POST ${DOC_URL}/proposals/prop-1/withdraw`,
    status: 200,
    body: { proposal: { ...PROPOSAL, status: "withdrawn" as const }, agreements: documentFixture(2) },
    call: (h) => h.withdraw("prop-1"),
  },
];

describe("useAgreements — every write invalidates the one key", () => {
  it.each(writes)("$name refetches the document after it lands", async ({ route, status, body, call }) => {
    const fetchMock = stubFetchRoutes({
      [`GET ${DOC_URL}`]: [
        { status: 200, body: { agreements: documentFixture(1) } },
        { status: 200, body: { agreements: documentFixture(2) } },
      ],
      [route]: { status, body },
    });

    const { result } = renderUseAgreements();
    await waitFor(() => expect(result.current.data?.version).toBe(1));

    await act(async () => {
      await call(result.current);
    });

    // The cache now holds the REFETCHED document, not the write's own response.
    await waitFor(() => expect(result.current.data?.version).toBe(2));
    expect(
      fetchMock.mock.calls.filter(
        ([input, init]) => String(input) === DOC_URL && (init?.method ?? "GET") === "GET",
      ),
    ).toHaveLength(2);
  });
});
```

Run it: `cd web && npx vitest run src/features/marriage/useAgreementsInvalidation.test.tsx` — expected PASS, 6 tests.

- [ ] **Step 7: Mount the block on the Retros page**

In `RetrosPage.tsx` the local import block is alphabetical, so the new import is a **new line 11**, before `MoodChart`:

```tsx
import { AgreementsToDiscuss } from "./AgreementsToDiscuss";
```

Then insert the mount immediately after line 236 (237 once the import is in) — the `)}` that closes the `noRetrosYet` ternary opened on line 153 — and before the modal comment that begins on line 238:

```tsx
      {/* A SIBLING of the ternary above, never inside either branch: the
          household most likely to have parked something has not started a
          retro yet, and a block inside either branch is invisible in the
          other (decision 7). It renders nothing at all when nothing is
          parked, so an ordinary Retros page is unchanged. */}
      <AgreementsToDiscuss />
```

- [ ] **Step 8: Give every test that mounts `RetrosPage` the agreements route, in this same change**

`stubFetchRoutes` throws on an unregistered request, TanStack Query absorbs that throw into `error`, and this block renders one muted line — so every one of these tests would stay green while the block silently errored on every render. Three components inside Retros alone have shipped that way.

`RetrosPage.test.tsx`, above `renderPage` (around line 99):

```tsx
// RetrosPage mounts AgreementsToDiscuss unconditionally (Task 16), which fires
// this GET on every render below. Nothing parked, so the block renders nothing
// -- AgreementsToDiscuss.test.tsx owns the assertions about it. Wrapped in its
// envelope, like every other agreements fixture: the hook parses
// { agreements: … } and an unwrapped body fails inside Zod.
const NO_AGREEMENTS = {
  agreements: {
    locked: false, owners: [], version: 1, updatedAt: null,
    sections: [], proposals: [], history: [],
  },
};
```

Then add `"GET /api/v1/marriage/agreements": { status: 200, body: NO_AGREEMENTS },` in three places: `renderPage`'s own map beside `"GET /api/v1/retros"` (line 105), and inside both direct `stubFetchRoutes({ … })` calls (lines 227-229 and 242-244). The two error-ladder tests get it too, deliberately: a later change to that ladder must not be able to resurrect the gap.

`router.test.tsx` needs it in exactly two of its four marriage tests — the two that actually mount the page. `:482` and `:624` redirect a capability-less member away, so `RetrosPage` never renders there and adding a stub would be noise. Add the same const beside `NO_SESSION` (line 57), then the route to the `stubFetchRoutes` calls at:

- `:513`, in `it("mounts the Retros page at /marriage/retros for a caller who has the marriage capability", …)`
- `:594`, in `it("redirects bare /marriage to /marriage/retros, its first page", …)`

- [ ] **Step 9: Run the marriage suite and the router suite**

```bash
cd web && npx vitest run src/features/marriage src/routes/router.test.tsx
```

Expected: PASS, with **no** `stubFetchRoutes: no stub registered for "GET /api/v1/marriage/agreements"` anywhere in the output. Grep the output for that string rather than trusting the summary line — an absorbed error keeps the suite green.

- [ ] **Step 10: Grep every mutation the hook returns for a caller outside it**

Goals shipped archive-and-restore with no screen calling it (`docs/LEARNING.md` pattern 15), which no test can see: each mutation is covered, and the screen that should use it simply does not. This is the first moment every consumer exists, so this is where the sweep runs.

```bash
cd /Volumes/Oink_Machine/Intelij/HouseholdDashboard/web
for m in createSection seedStarterSet propose agree park withdraw; do
  printf "%-15s " "$m"
  grep -rlE "(^|[^A-Za-z])${m}\(" src/features/marriage --include=*.tsx --include=*.ts \
    | grep -v "useAgreements.ts" | grep -v "\.test\." | tr '\n' ' '
  echo
done
```

Expected, one screen per line and `agree` on two:

| Mutation | Expected caller |
|---|---|
| `createSection` | `NewSectionModal.tsx` (Task 14) |
| `seedStarterSet` | `AgreementsPage.tsx` (Task 10) |
| `propose` | `ProposeAgreementModal.tsx` (Task 13) |
| `agree` | `AgreementsPage.tsx` **and** `AgreementsToDiscuss.tsx` |
| `park` | `AgreementsPage.tsx` (the card's `onPark`, wired in Task 12) |
| `withdraw` | `AgreementsPage.tsx` (the card's `onWithdraw`) |

**An empty line is a finding, not a grep to loosen.** Either wire that mutation to the screen the file table says owns it, or delete it from the hook — a mutation with no caller is untested product surface that reads as shipped.

- [ ] **Step 11: Mutation-check four claims, one per test that carries a claim**

Break each on purpose, watch the **named** test go red for the **named** reason, restore before the next one.

1. **The locked household's missing Agree.** Delete `p.canAgree && ` from the button's guard in `AgreementsToDiscuss.tsx` so it always renders. Expected: *"a locked household still sees the parked row, with no Agree button"* goes red on `not.toBeInTheDocument()` finding a button named `Agree` — a test failure. The other tests stay green, since `canAgree` is true in their fixtures.
2. **The parked-only filter.** Change `p.status === "parked"` to `p.status === "pending"`. Expected: *"renders nothing at all when the household has parked nothing"* goes red (the block appears for the pending fixture) **and** the first test goes red on `to-discuss-row-prop-1`. Two reds, both real.
3. **The shared invalidation.** In `useAgreements.ts`, delete `onSuccess: afterWrite` from `agreeMutation`. Expected: *"one Agree from the block refreshes both screens off one refetch"* goes red on `expect(documentGets()).toBe(2)` receiving `1`, with the block still on screen. This is the mutation the two-consumer test exists for: without it, both screens keep rendering a document one write out of date.
4. **The five writes nobody clicked here.** In `useAgreements.ts`, delete `onSuccess: afterWrite` from `parkMutation`. Expected: exactly one `it.each` row — *"park refetches the document after it lands"* — goes red on the same `toHaveLength(2)`, and the other five stay green, which is what proves the six rows are six claims and not one repeated.

All four are **test** failures, not build failures. If any prints `Failed to resolve import` or a type error instead, you deleted more than the line named.

- [ ] **Step 12: Full frontend check, then commit**

```bash
cd web && npx vitest run && cd .. && make lint
```

```bash
git add web/src/features/marriage/AgreementsToDiscuss.tsx \
        web/src/features/marriage/AgreementsToDiscuss.test.tsx \
        web/src/features/marriage/useAgreementsInvalidation.test.tsx \
        web/src/features/marriage/agreementCopy.ts \
        web/src/features/marriage/RetrosPage.tsx \
        web/src/features/marriage/RetrosPage.test.tsx \
        web/src/routes/router.test.tsx
git commit -m "feat(agreements): parked proposals on the Retros page, agreeable from there

The block reads the same GET, hook and query key as the Agreements page, so one
Agree refreshes both screens off one invalidation. Every existing test that
mounts RetrosPage gains the agreements route in this same change, the two router
tests included, because stubFetchRoutes throws on an unregistered request and
TanStack Query absorbs the throw into error state.

Mutations, all four test failures rather than build failures:
- deleted the p.canAgree guard on the Agree button: 'a locked household still
  sees the parked row, with no Agree button' went red on the button being
  present, and the other four block tests stayed green.
- changed the parked filter to 'pending': 'renders nothing at all when the
  household has parked nothing' went red, and so did the first test on
  to-discuss-row-prop-1.
- deleted onSuccess: afterWrite from agreeMutation: 'one Agree from the block
  refreshes both screens off one refetch' went red on 2 GETs expected, 1
  received, with the block still rendered.
- deleted onSuccess: afterWrite from parkMutation: only the park row of the
  six-write it.each went red; the other five stayed green.

Also swept every mutation the hook returns for a caller outside it (Step 10):
six mutations, six screens, agree on two."
```

---

### Task 17: The documents

**Files:** Modify `docs/SYSTEM_DESIGN.md`, `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`, `docs/HANDOVER.md`

**Interfaces:** Consumes Tasks 1–16 — the four tables, the port, the service, the seven routes and the two screens. Produces the four documents `CLAUDE.md` treats as part of the work rather than as tidy-up after it.

**No test, no run-and-fail and no mutation check in this task, deliberately.** Its equivalents are the recount script in Step 7, which is a count rather than an arithmetic adjustment, and the full suite in Step 11. Say so in the commit body rather than leaving a reader to wonder which step was skipped.

Line numbers below are from the tree as it stands at the start of this task; they move as you edit, so search for the quoted anchor text rather than trusting the number after your first insertion.

- [ ] **Step 1: `docs/SYSTEM_DESIGN.md` §6 — the four tables in the ER diagram**

Use the **`maintaining-system-design`** skill; this change trips four of its triggers at once (tables, routes, a new port, a reshaped flow).

In the relationship block, immediately after the `visions`/`vision_milestones` lines (around `:2424-2429`, ending `visions ||--o{ vision_milestones : has`), add:

```mermaid
    households ||--o{ agreement_sections : has
    agreement_sections ||--o{ agreements : groups
    households ||--o{ agreements : scopes
    households ||--o{ agreement_proposals : has
    agreement_sections ||--o{ agreement_proposals : "the section the change is about"
    agreements ||--o{ agreement_proposals : "target of an edit or remove (nullable, NO ACTION)"
    agreement_proposals ||--o{ agreements : "added by (NOT NULL) and removed by (nullable)"
    agreement_proposals ||--o{ agreement_signatures : collects
```

Then the four attribute blocks, in the style of `retro_actions` (`:2677`), after the `vision_milestones` block:

```mermaid
    agreement_sections {
        uuid id PK
        uuid household_id FK
        text name "NOT NULL — UNIQUE(household_id, name), mapped by constraint name so the screen can say 'you already have that section'"
        timestamptz created_at "NOT NULL, no DEFAULT now(): the starter set writes four rows in one transaction and stamps them strictly apart"
    }
    agreement_proposals {
        uuid id PK
        uuid household_id FK
        text kind "NOT NULL CHECK add|edit|remove — parsed in Go, the CHECK is a backstop"
        text status "NOT NULL CHECK pending|parked|accepted|withdrawn — no decline and no expiry (decision 6)"
        uuid section_id FK "NOT NULL — copied from the target on an edit or a remove, so a pending edit whose target is removed still has a card"
        uuid target_agreement_id FK "nullable — NULL for an add; FK added after agreements exists, the two tables name each other"
        text body "NOT NULL DEFAULT '' — empty for a remove"
        text previous_body "NOT NULL DEFAULT '' — the target's wording when proposed (decision 13): history's text, Restore's pre-fill, and the staleness check"
        text note "NOT NULL DEFAULT ''"
        text park_note "NOT NULL DEFAULT '' — may stay empty: Discuss is a bare button"
        uuid proposed_by_membership_id "NOT NULL, and NO foreign key — a log column (decision 20)"
        timestamptz created_at
        timestamptz resolved_at "nullable — NULL is open, retros.completed_at's shape; a CHECK keeps it and status in step"
    }
    agreements {
        uuid id PK
        uuid household_id FK
        uuid section_id FK
        text body "NOT NULL"
        uuid added_by_proposal_id FK "NOT NULL — propose then sign is the only path that can create one"
        uuid removed_by_proposal_id FK "nullable — a CHECK pairs it with removed_at, so a removal is one event"
        timestamptz removed_at "nullable — a removal is a stamp, never a DELETE (decision 9)"
        timestamptz created_at "ordering is created_at, id — no position column, see the notes below"
    }
    agreement_signatures {
        uuid proposal_id PK "also FK to agreement_proposals — composite PK with membership_id, ON DELETE CASCADE"
        uuid membership_id PK "a log column with NO foreign key (decision 20)"
        timestamptz signed_at "NOT NULL — an upsert keeps the FIRST stamp, so a repeat Agree is idempotent"
    }
```

- [ ] **Step 2: `docs/SYSTEM_DESIGN.md` §6 — the three prose notes a diagram cannot carry**

In the notes that begin around `:3010`, where `retro_actions`' own "no `position` column" note already lives, add three bullets. These are the lines an editor would otherwise "improve" into defects:

- **Nothing in Agreements is ever deleted.** A removed agreement keeps its row with `removed_at` and `removed_by_proposal_id` set, and every live read carries `WHERE removed_at IS NULL` (with a partial index for it) rather than filtering a growing tail. That is what makes the design's own promise true — "it stays in Version history, so you can always see it was there and restore it later" — and it is why there is no `DELETE` route and no `204` anywhere in this feature. Restoring is an ordinary add proposal (decision 18), not an `UPDATE` that clears the stamp.
- **No foreign key points at a retro row, and that is decision 7.** A proposal parked "for the next retro" is parked for one that usually does not exist yet, so the Retros page composes the block out of the same `GET /marriage/agreements` document the Agreements page reads, in the browser. `RetroService` and `AgreementService` share no port and no type. Whoever adds `retro_id` here will find every parked proposal in a household that has never started a retro has nowhere to point.
- **`agreement_proposals.proposed_by_membership_id` and `agreement_signatures.membership_id` carry no foreign key at all**, the shape `admin_audit_log.actor_user_id` already uses (decision 20). `CASCADE` — which `retro_action_assignees` uses — would silently un-sign an accepted agreement when a membership row went away, and the document would renumber itself with no record of why; `RESTRICT`, or a bare `REFERENCES` (which defaults to `NO ACTION`), would make removing an owner impossible after their first proposal, since members are hard-deleted here. The ids are verified at write time against `memberships` in the same household, and outlive the person leaving because the record of who agreed has to.

- [ ] **Step 3: `docs/SYSTEM_DESIGN.md` §3 — the port**

Add one row to the port table (`:697`), after `AdminDirectoryRepository`/`DatabaseBrowser` and before the unnumbered lookups:

| Port | Implemented by | Notes |
|---|---|---|
| `AgreementRepository` | `adapter/postgres` — one implementation across **two files**, `agreement_repo.go` (the pool-backed reads and the section writes) and `agreement_write_repo.go` (the two transactional writes) | **Twenty-seventh.** `Document` composes the whole screen in four list queries — sections, live agreements, open proposals, accepted proposals — and never a per-proposal round trip; the version, the `01..N` numbering and the awaiting lists are all derived above it, in `AgreementService`, from those four slices. Two methods run transactions and both do so because they write more than one row: `CreateProposal` writes the proposal **and** the proposer's own implicit signature (decision 5) — a proposal without it is one nobody has agreed to, including its author — and `Sign` reads the proposal's status, locks the *target agreement* row (decision 12, not the proposal: locking the proposal serialises two signatures on that proposal and nothing else, so an edit and a remove aimed at the same agreement would both pass their checks and both land), upserts the signature, applies the change when the signature completes the live owner set, and stamps the status, all on the transaction's own connection. Reaching back to the pool from inside either would ask for a second connection while holding the first — the defect `VisionRepository.Save` already carries a note about. A target that has moved since the proposal was written answers `domain.ErrAgreementChanged` and writes nothing; the section unique key is mapped by constraint name to `domain.ErrAgreementSectionNameTaken`, because a generic `ErrAlreadyExists` leaves the screen unable to say which collision happened |

- [ ] **Step 4: `docs/SYSTEM_DESIGN.md` §4 — seven routes and the guard label**

The guard graph at `:819` labels one edge `"retros, marriage/vision:<br/>marriage AND owner — reads included"`. Agreements joins the **same** group, so change the label rather than adding an edge:

```
    Cap -->|"retros, marriage/vision,<br/>marriage/agreements:<br/>marriage AND owner — reads included"| RequireCapRetro["requireCapability(marriage)<br/>then requireOwner, both ahead<br/>of the GET/HEAD check below"]
```

Then seven rows in the route table (`§4`), immediately after the two `/marriage/vision` rows at `:1128-1129`:

```markdown
| GET | `/marriage/agreements` | session · marriage · owner — one read composes the whole screen: sections, live agreements with their derived `01..N` numbering, open proposals with who each still waits for, and the accepted-proposal log the version history modal renders. There is no separate history route, because the version is `count(accepted) + 1` and the service has already walked that slice to compute it. Not gated on `locked`: a household that has dropped below two owners still gets `200` and its whole document, read-only (decision 3), and one that has never had two owners gets `locked: true` with empty arrays — a consequence of its rows, not a special shape |
| POST | `/marriage/agreements/sections` | session · marriage · owner · CSRF — `201`, because it creates a row. A section is a label, not a promise: creating one is immediate and unsigned (decision 8). A duplicate name is `409 AGREEMENT_SECTION_NAME_TAKEN`, mapped from the unique constraint by name |
| POST | `/marriage/agreements/starter-set` | session · marriage · owner · CSRF — `200`, not `201`: it is idempotent (`ON CONFLICT DO NOTHING` over the four labels) and may create nothing. It seeds **sections only** (decision 17), so "everything on this page is here because you both agreed" stays literally true |
| POST | `/marriage/agreements/proposals` | session · marriage · owner · CSRF — `201`. The handler parses `kind` itself (decision 21), so a bad request body is `422 AGREEMENT_KIND_INVALID` and a bad database column is a fault, never the same sentinel; it blanks `sectionId` for any kind but `add`, the section being the target's, and stamps household and proposer from the route and the session. One transaction writes the proposal and the proposer's own signature |
| POST | `/marriage/agreements/proposals/{id}/agree` | session · marriage · owner · CSRF — `200`. Each action is its own POST rather than a patchable `status`, or saving a note could withdraw a proposal. The signature is an upsert, so a repeat Agree is idempotent and still closes the proposal if the set is now complete (decision 16); a target that has changed since the proposal was written is `409 AGREEMENT_CHANGED` and nothing is written |
| POST | `/marriage/agreements/proposals/{id}/park` | session · marriage · owner · CSRF — `200`. Discuss, in the browser's words: the proposal stays open and appears in the read-only block on the Retros page. The note may be `""`, but the body is always sent — an absent one is `400 INVALID_BODY` |
| POST | `/marriage/agreements/proposals/{id}/withdraw` | session · marriage · owner · CSRF — `200`, and no DELETE anywhere in this feature (decision 9). The proposer's, until the proposer is no longer an owner, when any owner may withdraw it (decision 15) — without that fallback a proposal left behind by a departed partner could never be removed by anyone. Refusal precedence is `404` → `403 AGREEMENT_NOT_PROPOSER` → `409 AGREEMENTS_NEED_TWO_OWNERS` (decision 22), because the handler must read the proposal before it can know whose it is |
```

- [ ] **Step 5: `docs/SYSTEM_DESIGN.md` §5 — the propose → sign flow**

Insert a new flow **before** `### What the frontend loads` (`:2252`), in the shape the Retros flow at `:2031` uses. This is the first request flow in this product with two actors and a real gap between them, which is the reason it earns a diagram at all:

````markdown
### Agreements — propose → sign, the first flow with two actors and a gap between them

```mermaid
sequenceDiagram
    participant B1 as Browser (Andreas)
    participant B2 as Browser (Christine)
    participant H as Handler
    participant Svc as AgreementService
    participant Repo as AgreementRepository
    participant DB as Postgres

    B1->>H: POST /api/v1/marriage/agreements/proposals<br/>{ kind: "add", sectionId, body }
    H->>H: ParseAgreementProposalKind(body.Kind) -- 422 here,<br/>never a shared sentinel with a corrupt column
    H->>Svc: Propose(householdID, membershipID, proposal, now)
    Svc->>Repo: owners of this household, live
    Note over Svc: Fewer than two owners -> ErrAgreementsNeedTwoOwners<br/>BEFORE any repository write (decision 1)
    Svc->>Repo: CreateProposal(AgreementProposalWrite{...})
    Repo->>DB: BEGIN
    Repo->>DB: INSERT agreement_proposals (status 'pending')
    Repo->>DB: INSERT agreement_signatures (this proposer)
    Repo->>DB: COMMIT
    Note over Repo,DB: Two rows, one transaction (decision 5):<br/>proposing IS agreeing, and a proposal without<br/>its author's signature is one nobody has agreed to
    Svc->>Repo: Document(householdID) -- recomposed AFTER the write
    Svc-->>B1: 201 { proposal: {... awaitingNames: ["Christine"]}, agreements: {...} }

    B2->>H: GET /api/v1/marriage/agreements
    H->>Svc: Get(householdID)
    Svc->>Repo: Document(householdID) -- four list queries
    Svc->>Svc: version = count(accepted) + 1; 01..N numbering across<br/>sections; AwaitingSignature(owners now, signed) per proposal
    Svc-->>B2: 200 { agreements: { version: 1, proposals: [...] } }

    B2->>H: POST /api/v1/marriage/agreements/proposals/{id}/agree
    H->>Svc: Sign(householdID, proposalID, membershipID, now)
    Svc->>Repo: Sign(AgreementSignatureWrite{...})
    Repo->>DB: BEGIN
    Repo->>DB: SELECT status -- already resolved? ErrAgreementNotOpen
    Repo->>DB: SELECT ... FROM agreements WHERE id = target<br/>AND removed_at IS NULL AND body = previous_body FOR UPDATE
    Note over Repo,DB: The lock is on the AGREEMENT, not the proposal<br/>(decision 12): two proposals against one agreement<br/>are ordered with respect to each other. A miss on any<br/>of the three predicates is ErrAgreementChanged and<br/>nothing is written (decision 13)
    Repo->>DB: INSERT agreement_signatures ... ON CONFLICT DO NOTHING
    Repo->>DB: owners of this household, re-read INSIDE this transaction
    Note over Repo,DB: The signing set is every CURRENT owner, evaluated<br/>live (decision 4) -- an owner who joined mid-proposal<br/>must sign, one who left stops blocking it
    Repo->>DB: every owner signed -> INSERT agreements / UPDATE removed_at,<br/>then UPDATE the proposal to 'accepted', resolved_at = now
    Repo->>DB: COMMIT
    Svc->>Repo: Document(householdID)
    Svc-->>B2: 200 { proposal: { status: "accepted" }, agreements: { version: 2, ... } }

    B1->>H: GET /api/v1/marriage/agreements (Andreas's next paint)
    Svc-->>B1: 200 -- the agreement is live, numbered, and in the history list
```

**Every write returns the whole document**, not only the row it touched, because
each one moves four things at once: the version, the `01..N` numbering, the
history list and which proposals are still open. It is composed *after* the
transaction commits, so it is a snapshot a concurrent Agree may already have
overtaken — which is why the frontend invalidates one query key and refetches
rather than trusting what it got back. The `proposal` beside it is the only
place `accepted` or `withdrawn` reaches the wire, and no screen renders it; it
exists so the HTTP tests can assert the transition field by field.

**The signing set is recomputed on every signature rather than snapshotted at
proposal time**, and that is the decision most likely to be "simplified" later.
Snapshotting means a proposal can be completed by people who no longer live in
the household, which is the wrong answer to the question this feature exists to
ask. The cost is the one decision 16 pays for: a proposal can become fully
signed by nobody's action — three owners, one proposes, one agrees, the third
leaves — so `canAgree` stays true when the awaiting list is empty, and one
idempotent re-Agree closes it. No background job, no sweeper.

**The version is `count(accepted proposals) + 1`, derived on every read.** A
stored counter can drift from the rows it counts, the same reasoning that kept an
analytics table out of the admin metrics screen. The numbering is derived at
render for the same family of reasons (decision 11): an ordering integer needs a
writer, the only safe writer is `max(position) + 1` inside the insert, two owners
can still collide on it, and no reordering control is drawn anywhere — so the
column would exist only to create the race.
````

Then, in **What the frontend loads** (`:2252`) and §7's tree, three smaller edits that are easy to miss:

- The Retros page entry in that section now fires `GET /marriage/agreements` as well as `GET /retros`, because `RetrosPage` mounts `AgreementsToDiscuss` unconditionally — and it is the *same* query key `/marriage/agreements` uses, so a household that visits both pages in one session pays for one fetch, not two.
- §7's `marriage/` tree entry (`:3186` onwards) gains `AgreementsPage` — the four pre-document states, the sections grid, the proposal block above it, the three modals, and `useAgreements`/`agreementQueryKeys.ts`/`agreementCopy.ts` in the shape the Vision entry already describes — plus `AgreementsToDiscuss` under the Retros paragraph, named as **frontend composition with no Go-level coupling**.
- The route prose at `:3483-3500` says Marriage has two children and `SPACE_PAGES.marriage` has two links. It now has **three** (`/marriage/agreements`, `AgreementsPage`), and the bare-`/marriage` index still redirects to Retros — first page wins, unchanged.

- [ ] **Step 6: `docs/FEATURE_TRACKER.md` — the rows**

Six rows in §6 move ⬜ → ✅, at `:1454-1459`:

```markdown
| Agreements by section | ✅ |
| Agreements empty state with starter sets | ✅ *("Use starter set" seeds the four section labels — Money, Conflict, Home & kids, Us — and **no agreements** (spec decision 17). Every agreement without exception arrives through propose → sign, so "everything on this page is here because you both agreed" stays literally true, with no bulk-signed exception to explain. An empty section is invisible in the document (decision 8), so the screen names the four it just created rather than showing four empty headings)* |
| Propose a change — add, edit, remove (modal) | ✅ |
| New agreement section (modal) | ✅ |
| Version history (modal) | ✅ |
| Agreements locked until a second owner | ✅ *(both halves: a household that has **never** had two owners gets the explanation and an "Invite your partner" deep link into the existing Settings invite flow, and one that **had** two owners keeps its whole document, read-only, with the proposals still listed and named as waiting (decision 3). The write controls are hidden, not disabled — a disabled control that cannot say why is the defect the admin flags screen already carries)* |
```

`:1450`, "Vision — marriage duration beside the theme", **stays ⬜** and keeps the reason already recorded there: it is Vision's, deliberately unbuilt, and nothing in this work touched it.

Then three rows the design never draws, added after the six — the treatment Goals' contributions and Accounts' archive/restore already have:

```markdown
| Park a proposal for the next retro (no mockup — see below) | ✅ *(Discuss moves a proposal to `parked`, where it stays open and answerable, and it appears in a read-only "To discuss" block on the Retros page — decision 7. **No writes into retro tables and no foreign key to a retro row**: the next retro usually does not exist yet, and the block is frontend composition off the same `GET /marriage/agreements` document, sharing one query key so agreeing in either place refreshes both. The block lists parked proposals only, and offers Agree but neither Discuss nor Withdraw — a reminder of what the retro should cover, not a second editing surface)* |
| Restore a removed agreement from version history (no mockup — see below) | ✅ *(the history entry for a removal offers Restore, which opens Propose in add mode pre-filled with the removed wording — an ordinary proposal every owner still has to agree, decision 18. Anything else would let one owner put back alone what two owners agreed to take out. It works because nothing is ever deleted: a removal is a stamp on the row, decision 9)* |
| "Popular starting points" — tapping a card | ⬜ *(drawn, deliberately not built. The four cards ship as read-only illustration on the first empty state: the design draws hover styling, gives them no `onClick`, and nothing says what tapping one would propose — and every agreement has to arrive through propose → sign, so a card that added one silently would be the one exception to the promise this page makes. Same treatment as the ⌘K chip and the "45 min" retro duration)* |

Also update the stale sentence at the end of §6's prose — "**Settled 2026-09-05 by the product owner, before Agreements' spec is written** … it is added as ⬜ in the same table" — which stops being true the moment the row above flips. Leave the paragraph (it records the decision and the three options weighed) and add one dated line beneath it: the spec was written 2026-09-05, the feature shipped, and the locked-state row is now ✅ with both of its halves named.

- [ ] **Step 7: `docs/FEATURE_TRACKER.md` — recount by counting symbols**

Never adjust the previous totals. This file records that adjusting by delta has produced wrong numbers here before, in both directions at once. Run the file's own rule — the first status symbol in each row's own cell:

```bash
cd /Volumes/Oink_Machine/Intelij/HouseholdDashboard
for s in "✅" "🟡" "⬜" "🚫"; do
  printf "%s " "$s"
  awk '/^## 6 · Marriage/,/^## 7 · Family/' docs/FEATURE_TRACKER.md | grep -c "| $s"
done
```

It reads **10 / 0 / 7 / 0** before this task. Write what it prints into the summary table's Marriage row (`:812`), then re-sum the **Total** row (`:816`) from the nine section rows **as they stand**, and update the "Where things stand" headline (`:16`) to Built + Partial of the new total. Add a dated paragraph above the table saying the recount was a count, in the shape of the ones already there.

Checks, not values to type in: §6 should come to **18 / 0 / 2 / 0**, the Total to **88 / 17 / 16 / 3 = 124**, and the headline to **105 of 124**. If any of those disagrees with what the script prints, the script is right — find the row the difference is in rather than editing a number to match this plan.

- [ ] **Step 8: `docs/LEARNING.md` — what this round taught**

One entry per defect worth remembering. Two are already visible from the design review, and both belong in **existing** sections rather than a new one — the repetition is the point:

- **Pattern 5, "Silent partial success is worse than loud failure"** (`:1828`): proposing is two rows, the proposal and the proposer's own implicit signature (decision 5). A proposal written without its signature is one nobody has agreed to, *including its author* — and nothing on the screen would say so, because the card renders "needs Christine" off the awaiting list either way. Another instance in that list, and the reason the write is one repository method running one transaction rather than a service calling two.
- **The "Database and repositories" catalogue** (`:3200`), beside the existing `pgx.BeginFunc` and pool-starvation evidence (the `VisionRepo.Save` entry at `:3365`): **a row lock only serialises the row it takes.** The first sketch of `Sign` locked the proposal, which orders two signatures on *that* proposal and nothing else — so an edit and a remove aimed at the same agreement would each pass their own checks and both land, producing a duplicated agreement or a removal recorded against wording nobody agreed to. The lock is taken on the *target agreement* instead (decision 12), and the owner set is re-read inside the same transaction rather than trusted from a count taken before it. Found in design review, not by a test: no test in this codebase drives real concurrent database load, which is the same reason the pool-starvation entry above it was found by reading rather than by going red.

Add whatever else this round actually produced, with the symptom and what would have caught it sooner. **If it produced no further defect worth recording, say so in the commit body rather than inventing one** — the tenth entry nobody hit is noise that makes the nine real ones cheaper to skim. The walk in Task 18 will add its own, after it has run.

- [ ] **Step 9: `docs/HANDOVER.md` — the slice table and what is next**

Two places, both of which become false the moment this ships:

- **§1's slice table, `:112`** — the "3 — Marriage" row ends "**Agreements: not started — Marriage's last feature** … The question it was blocked on … is settled 2026-09-05". Replace that clause with what is now true: built across eighteen tasks, the four tables, the port, the service, the seven routes and the two screens; the propose → sign lifecycle with every change agreed by every current owner; nothing ever deleted; parked proposals surfacing on Retros. Leave the walk sentence for Task 18 to write, or write it there — do not claim a walk that has not run.
- **§4, around `:619-640`** — "the next work is Agreements" and the paragraph naming the one-owner question. Marriage is now complete, so §4's next work is whatever the "Suggested order" in `docs/FEATURE_TRACKER.md` names after slice 3; say which, and keep the settled decision (fewer than two owners, no Agreements) as recorded history rather than deleting it.

- [ ] **Step 10: Check the three documents against the tree, not against this plan**

```bash
cd /Volumes/Oink_Machine/Intelij/HouseholdDashboard
grep -n "agreement" docs/SYSTEM_DESIGN.md | head -40
grep -rn "Agreements" docs/FEATURE_TRACKER.md | grep "⬜" 
```

Every ⬜ that comes back should be one you meant to leave: the marriage-duration row and the "Popular starting points" behaviour, and nothing else.

- [ ] **Step 11: Full suite and lint**

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

Expected: both green. Fix anything that is not before committing — this is the bar `CLAUDE.md` sets, and the walk in Task 18 starts from a green tree.

- [ ] **Step 12: Commit**

```bash
git add docs/SYSTEM_DESIGN.md docs/FEATURE_TRACKER.md docs/LEARNING.md docs/HANDOVER.md
git commit -m "docs: Agreements where each of the four documents is actually read

SYSTEM_DESIGN: four tables in the §6 ER diagram with their attribute blocks and
three prose notes a diagram cannot carry (nothing is ever deleted; no FK points
at a retro row; the two membership columns carry no FK at all), the twenty-
seventh port in §3, seven routes and the widened guard label in §4, and a new §5
flow — the first in this product with two actors and a gap between them.
FEATURE_TRACKER: six §6 rows ⬜ → ✅, three rows the design never draws (park,
restore, and 'Popular starting points' as drawn-but-not-built), and the summary
recounted by counting symbols rather than adjusting the previous totals — §6
18/0/2/0, Total 88/17/16/3 = 124, headline 105 of 124.
LEARNING: two entries, both filed under existing patterns — the implicit
signature as a second row under pattern 5, and 'a row lock only serialises the
row it takes' beside the transaction and connection-pool evidence.
HANDOVER: the slice-3 row and §4's next work.

No mutation check in this task: it changes no code. Its equivalents are the
recount script, which counts rather than adjusts, and make lint && make test,
both green."
```

---

### Task 18: The browser walk

**Files:**
- Create: `docs/superpowers/plans/2026-09-05-hearth-agreements-verification.md` and `docs/superpowers/plans/2026-09-05-hearth-agreements-screenshots/`
- Modify: whatever the walk finds — **fixed here, not filed for later** — plus `docs/FEATURE_TRACKER.md` and `docs/LEARNING.md`, which gain the walk's own result after it has run (Step 7)

**Interfaces:** Consumes the whole feature running under `make up` against a real database. Produces the verification record the Definition of Done names, and the two document amendments that can only be written once the walk has happened.

**No test, no run-and-fail and no mutation check, deliberately.** The fifteen-criterion walk *is* this task's verification, and it catches a class of defect the suite cannot: every one of the last five walks in this project found something, and two of those found the same shape in a sibling afterwards. Say so in the commit body.

- [ ] **Step 1: Start clean, on the engine that is actually serving**

```bash
cd /Volumes/Oink_Machine/Intelij/HouseholdDashboard
make down && make up && make seed
lsof -nP -iTCP:5173 -sTCP:LISTEN
docker compose exec -T postgres psql -U hearth -d hearth -c "select max(version_id) from goose_db_version;"
```

`make down && make up` forces recreation, which is what makes the `migrate` service rerun — a stack left running across a new migration keeps its already-succeeded migrate container and you walk yesterday's schema. The `psql` line must read **14** (`00014_agreements.sql`); if it reads 13, the migration did not apply and nothing below is meaningful.

**This machine runs two Docker engines** (Docker Desktop and colima) and a stack on one silently holds 5173/8080/8025 out from under the other, so the `lsof` line tells you which process is answering before you trust anything `localhost` shows. If an edit under `web/src/**` appears not to compile, ask Vite for the module directly — `curl -s http://localhost:5173/src/features/marriage/agreementCopy.ts | grep toDiscussTitle` — and `docker restart hearth-web-1` if what it serves does not match the file on disk (`docs/HANDOVER.md` §2; `touch` every file you edited before trusting the browser at all).

**`make seed` leaves the second owner as a pending invite, so a freshly seeded household IS the locked state** — that is criterion 1's fixture, not a bug, and criterion 3 is where it stops being locked.

- [ ] **Step 2: Attach a browser**

Playwright MCP, with the Vision walk's own fallback rule: if `tabs_context_mcp` answers "Browser extension is not connected" three times, stop trying Claude in Chrome and drive Playwright's own Chromium for the whole walk, including the two-session criteria. If either browser refuses to attach because a previous session left one running, kill it **scoped to the automation profile path**, never by process name:

```bash
pkill -f "user-data-dir=/Volumes/Oink_Machine/Library/Caches/ms-playwright-mcp/mcp-chrome-f265c67"
```

Two rules for everything below. **Assert on numbers read in page script** — `getBoundingClientRect()`, `innerText` — never on how a scaled screenshot looks; a walk here twice concluded an element was missing when it was rendered far right at a tiny scale. And **React controlled inputs ignore synthetic typing**:

```js
const set = Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype, 'value').set;
set.call(el, 'No solo spend over $200');
el.dispatchEvent(new Event('input', { bubbles: true }));
```

`HTMLInputElement` for the section-name field, `HTMLSelectElement` with `'change'` for the section picker.

- [ ] **Step 3: Walk the fifteen criteria**

Sign in with what `make seed` printed.

- [ ] 1. `/marriage/agreements` on the freshly seeded household explains the two-owner rule, and **"Invite your partner"** lands on Settings **with the invite modal open**. Then load `/settings?invite=maybe` directly: Settings, modal **closed**. The deep link is this feature's own new work, so a bare navigation to Settings is not a pass.
- [ ] 2. Cold-load `/marriage/agreements` as a **two-owner** household with the document request held open, and assert the locked explanation never appears — not even for one frame. Hold it deterministically rather than throttling:

  ```js
  await page.route('**/api/v1/marriage/agreements', async (route) => {
    await new Promise((r) => setTimeout(r, 3000));
    await route.continue();
  });
  ```

  During the hold, `document.querySelector('[data-testid="agreements-locked-invite"]')` must be `null` and something must say the page is loading. Nothing renders from a guess: `data?.locked ?? true` flashes "you need a second owner" at a two-owner household on every cold load, and a walk that navigates and waits cannot see it.
- [ ] 3. Accept the invite `make seed` printed, in a second browser profile: the page is the unlocked **empty** state, not the locked one and not an error.
- [ ] 4. Click **Use starter set**. The page does **not** look identical afterwards — an empty section is invisible (decision 8), so this is exactly the button that can appear to do nothing — and all four sections are offered in the propose modal's section picker.
- [ ] 5. Propose as the first owner: the card names who it waits for, offers **Withdraw**, offers **no Agree**. Withdraw one and watch it go.
- [ ] 6. The same card in the second profile offers **Agree** and **Discuss**, and no Withdraw.
- [ ] 7. Agree it: the agreement appears numbered in its section, and the section count, the continuous `01..N` numbering across both columns and the header's `v{n}` all move together. Read all three in page script in one go — a total and its breakdown that apply different filters stop reconciling quietly.
- [ ] 8. Discuss a proposal, open `/marriage/retros`, agree it from the To-discuss block. **Both** pages reflect it with no manual reload — navigate back to Agreements without reloading and confirm the proposal is gone and the agreement is there.
- [ ] 9. On a household that has **never** started a retro, `/marriage/retros` still renders the To-discuss block. (Two mutually exclusive branches; a block inside either is invisible in the other.)
- [ ] 10. Two browsers, one agreement, two edit proposals. The literal sequence: A opens Propose → edit against agreement 01 and sends; B, whose modal was opened before A's change landed, sends its own edit against the same wording; B is refused with the conflict copy, **nothing B typed is lost**, the send button stays disabled after the page refetches, and B's proposal is still listed. If the refusal arrives at propose time rather than at sign time, that is the propose-time freshness check doing its job — record it as the interpreted path it is, with the sentence the screen actually showed.
- [ ] 11. Propose a remove, agree it, open **Version history**, read the removed wording, click **Restore**: Propose opens seeded with that wording in add mode, and the picker offers the now-empty section.
- [ ] 12. With proposals open, remove the second owner in Settings: the page re-locks, the proposals are **still listed** and named as waiting, and **no write control is offered** — not disabled, gone. A household that loses sight of its proposals will assume they were deleted.
- [ ] 13. Create a section named "Money" twice: the second says so under the field, the modal stays open, and the typed name survives. A generic red box four clicks in is the empty-`<select>` failure again.
- [ ] 14. Keyboard only: Tab reaches Agree, Discuss and Withdraw on a pending card and every control in the propose modal, each with a **visible** focus ring, and Enter activates. Read the ring rather than trusting the class: `getComputedStyle(document.activeElement).outlineWidth` on each stop.
- [ ] 15. The ladder 320 / 360 / 375 / 414 / 768 / 1024 / 1440 on the populated page **and** the history modal, comparing `scrollWidth` to `clientWidth`. Read the `<dialog>`'s own numbers — it paints in the top layer, and a document-level check cannot see it:

  ```js
  const d = document.querySelector('dialog[open]');
  ({ page: document.documentElement.scrollWidth - document.documentElement.clientWidth,
     dialog: d ? d.scrollWidth - d.clientWidth : null });
  ```

  A numbered row plus a section label plus body text plus two buttons is the shape that has overflowed here before.

- [ ] **Step 4: Compare screenshots by hash, never by eye**

Save every before/after pair into `docs/superpowers/plans/2026-09-05-hearth-agreements-screenshots/` and compare:

```bash
shasum -a 256 docs/superpowers/plans/2026-09-05-hearth-agreements-screenshots/*.png
```

Two identical hashes mean the change did not land — a previous round's fix was caught half-wrong by two byte-identical screenshots that looked different.

- [ ] **Step 5: Fix what the walk finds here, then walk it once off-script**

Every walk in this project has found something, and the ones that found it in *one* place have twice found the same shape in a sibling — **grep for the shape of each defect before closing it** (`hunting-sibling-defects`). Fix it on this branch rather than filing it: a defect filed at the end of a feature is a defect nobody has the context to fix later.

Then use the product for ten minutes without the list: propose what you would actually propose, park it, come back to it the way a household would, on a phone-width window. **A walk derived entirely from a spec has already passed here while the spec was wrong** (`docs/LEARNING.md` pattern 13). Record what that turns up too, even where no criterion covers it.

- [ ] **Step 6: Write the record**

`docs/superpowers/plans/2026-09-05-hearth-agreements-verification.md`, criterion by criterion, in the shape `2026-08-28-hearth-vision-verification.md` uses: the headline result first, then per criterion what was done, what was observed, and — for anything met by an interpreted rather than a literal path — say so plainly inline rather than passing over it quietly. Name the engine (`colima status`), the `goose_db_version` you read, which browser actually drove the walk, and anything that cost time and would cost it again.

- [ ] **Step 7: Amend the two documents that could not be written before the walk**

- `docs/FEATURE_TRACKER.md` §6 prose: a dated paragraph in the shape the Retros one already takes ("**the feature's own fifteen-criterion browser walk … has now run and passed, 15 of 15** (2026-08-18)"), naming the date, the result, and any criterion met by an interpreted path — Retros' own prose does exactly that for its criteria 2, 10 and 13. If the walk found a defect and you fixed it, say what it was: that sentence is what makes the ✅ rows above it mean "built **and verified**", which is what the legend says.
- `docs/LEARNING.md`: the walk's own section under "Tooling and infrastructure", in the shape of "Vision's fifteen-criterion browser walk (2026-08-29)" (`:4766`), plus one entry per defect the walk actually found, filed under the existing pattern it matches rather than a new section. **If it produced none worth recording, say so in the commit body rather than inventing one.**

- [ ] **Step 8: Full suite and lint, after the fixes**

```bash
export PATH=/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

Expected: both green. Task 17 ran this on a tree the walk had not touched yet, so this run is not a repeat — anything the walk fixed is code the suite has not seen.

- [ ] **Step 9: Commit**

```bash
git add docs/superpowers/plans/2026-09-05-hearth-agreements-verification.md \
        docs/superpowers/plans/2026-09-05-hearth-agreements-screenshots/ \
        docs/FEATURE_TRACKER.md docs/LEARNING.md
git commit -m "docs: the Agreements walk, criterion by criterion

Fifteen criteria against a real database on a freshly seeded household, with the
second owner joining through the real invite. The tracker's §6 prose and
LEARNING's walk section are written here rather than in Task 17, because ✅ in
that file means built AND verified and neither sentence could be true before
this ran.

No mutation check in this task: the walk is the verification, and it reaches the
class of defect the suite cannot — a spec-derived walk has already passed here
while the spec was wrong, which is why Step 5 also uses the product off-script."
```

Any code fix the walk produced is committed separately, before this one, with its own message naming the criterion that found it and the grep that swept for siblings.
