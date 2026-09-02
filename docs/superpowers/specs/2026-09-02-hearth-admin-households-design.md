# Hearth — admin households and metrics

Written 2026-09-02. Second slice of the platform admin surface; the first is
`2026-09-01-hearth-admin-surface-design.md`, whose §6 this spec expands from
three bullets into something a plan can be written from. Where the two
disagree, this one wins for households, and §6 of the older spec now points
here.

The operator — one person, the vendor — runs a live install that strangers
sign up to unsupervised. Today every question about that install is answered
with `psql` over SSH: how many households exist, who signed up this week, did
a sign-up finish or die at the email step, which invites are still waiting
for a link to be handed over by hand, and — when someone writes "I can't sign
in" — does the account exist, which household is it in, has it ever signed
in, is it locked. This spec builds the screen that answers those questions.

---

## 1. What this is not

**Not flag targeting.** The admin surface spec's §3.6 flags screen can show
and remove a household's flag override but cannot create one, because no
household picker exists anywhere in the product. This slice builds the list
that picker would read, but deliberately not the control. The product owner
chose to build support lookup and install metrics first and flag targeting
after; the flags row in `docs/FEATURE_TRACKER.md` §9 stays 🟡 with its gap
reworded, not closed. Adding the control later is a section on the drill-in
page and one mutation hook, not a redesign.

**Not money.** No balance, transaction, budget, goal or bill appears on
either screen, and a test asserts the response key set exactly so one cannot
arrive by accident. Financial data stays behind the read-only database
browse (admin spec §4), which costs a deliberate second step and leaves a
second audit row. Casually reading a customer's finances must never be one
click from a support question.

**Not an actions panel.** Nothing here writes to a household: no edit, no
delete, no unlock, no resend, no impersonation. The one write in this slice
is a timestamp on the caller's own session, and it is not an admin action at
all. `adminctl` keeps every operator write, as the admin surface spec's ADR
argued.

---

## 2. Decisions

Each was put to the product owner with its trade-off on 2026-09-02, in the
brainstorm this spec came out of.

1. **Support lookup and install metrics first; flag targeting deferred.**
   The two jobs this screen is opened for are "who is this person" and "is
   anyone using this". Flag targeting is real but rarer, and it is a small
   addition once a household list exists. See §1.

2. **One search box, over households and members.** "Can't sign in" arrives
   with an email address, not a household name. The box matches household
   name, family name, member display name and member email, case-insensitive
   substring. A result row is always a household; when a member matched
   rather than the household, the row names that member so the operator can
   see why it appeared. Telegram-only accounts have no email (`users.email`
   is NULL for them) and the product stores only their `chat_id`, which the
   operator will not know — they are found by name only. Accepted.

3. **`sessions.last_seen_at`, touched at most once an hour; the label is
   "Last active".** Sessions live 30 days and are extended in place only
   inside their last 24 hours, so `sessions.created_at` is the last *sign-in*
   and a daily user on a phone may not sign in for months. Metric B — "is
   anyone using this" — would read every such household as churned. A
   throttled touch in the session middleware costs one `UPDATE` per
   session-hour and answers the real question. The alternative, `created_at`
   labelled "Last sign-in", was offered and declined. The admin surface spec's
   "no analytics table" rule is about counters that drift from the rows they
   count; a timestamp on the row it describes is a fact, not a counter.

4. **Four tiles, not five.** Households; active in the last 7 days; sign-ups
   in the last 30 days, requested and completed; invites pending. A members
   total was dropped because nobody acts on it. It is one `COUNT(*)` away if
   that changes.

5. **Explicit search — Enter or a button, never on keystroke — and the query
   goes into the audit row.** Every request under `/admin` writes an
   `admin_audit_log` row before its handler runs. Live search would write a
   row per debounce tick, the same noise the first admin slice found with
   refetch-on-focus and turned off. One submitted search is one request, one
   row; and "the operator searched for `christine@`" is a fact the log should
   hold. `auditAdmin` today leaves `Detail` empty because chi has not parsed
   route parameters when the row is written — but `r.URL.RawQuery` is
   available there, so the middleware records it as `Detail.query` when it is
   non-empty. This applies to every admin route, not only search; that is
   the point of doing it in middleware.

6. **The drill-in shows the household's sign-in lockout.** Three wrong
   passwords in fifteen minutes lock a household's password sign-in for
   fifteen minutes (`domain.DefaultLockoutPolicy`, household-wide by an
   earlier owner decision). A locked member sees a countdown and writes to
   support; without this line the operator cannot tell locked from
   wrong-password from no-such-account without `psql`. The line reuses
   `LoginAttemptRepository.FailuresSince` and `LockoutPolicy.Evaluate`, the
   same call sign-in makes, so "locked" has exactly one definition. Shown only
   while locked. Offered as droppable; kept.

7. **The drill-in is read-only.** Members, roles, capabilities, channel, last
   active, pending invites, lockout state. See §1.

8. **A new service and a new narrow port, not a bigger `AdminService`.**
   `AdminService` today is "who is a platform admin, feature flags, the audit
   log". Adding cross-household reads would make describing it need the word
   "and", which `CLAUDE.md` names as the moment to split. Composing the reads
   from the existing household-scoped ports was also considered and rejected:
   every one of them answers "one household", so listing all would mean new
   methods on four ports plus N+1 calls for counts. The port has one
   production implementation and one in-memory double; the boundary it draws
   — reads across every household — exists nowhere else in the product, which
   is what makes it worth naming.

9. **The list page is one request.** Metrics and rows come back together
   from `GET /admin/households`, so one page view is one audit row. The four
   counts are cheap and a search re-fetching them costs nothing worth
   avoiding.

10. **The sign-up counter's window must sit inside prune retention.**
    `adminctl prune` deletes consumed and expired sign-up rows older than
    `--older-than`; the floor is 7 days and `deploy/README.md` runs it at 30.
    A 30-day counter therefore loses at most its edge day, and only if prune
    ran that morning. Documented in the runbook rather than enforced: a
    counter that is occasionally one day short is not worth a coupling
    between a CLI flag and a query constant.

11. **No configuration gate.** The admin surface spec's §9 says its items 4,
    5 and 6 each sit behind their own configuration; that is true of the
    database browse and the message inspector, which need a role and a URL,
    and false of this one, which reads tables that already exist. The screen
    is always available to a granted admin. §9 of that spec is corrected to
    say so.

---

## 3. Data model

One migration, `00013_session_last_seen.sql`:

```sql
ALTER TABLE sessions ADD COLUMN last_seen_at timestamptz;
```

Nullable, no default, no backfill, no index. A NULL means "not touched since
this column existed", and every reader treats it as `created_at`:
`COALESCE(last_seen_at, created_at)` is the one expression for "when was this
session last used", and it appears in exactly one place in the SQL file so it
cannot drift.

No index yet. The install is one box and a few households; the "active in
the last 7 days" query is an `EXISTS` per household over a table that is
small by construction (one row per sign-in, pruned by expiry). An index on
`(household_id, COALESCE(last_seen_at, created_at))` is the fix when a query
plan shows it is needed, not before.

**The touch rule**, in `middleware_session.go`, beside the existing extend:

```go
// sessionTouchInterval is how stale sessions.last_seen_at may be before
// a request refreshes it. One write per session-hour rather than one per
// request: the column answers "was this household active this week",
// which an hour's resolution serves completely, and the session
// middleware runs on every authenticated request.
const sessionTouchInterval = time.Hour
```

After `ByTokenHash` succeeds: if `record.LastSeenAt` is nil, or `now` minus
it is at least `sessionTouchInterval`, call `Sessions.Touch(ctx, hash, now)`.
On error, `slog.Warn` and continue — exactly what extend does. A request never
fails because a usage timestamp could not be written, and the next request
will try again.

`Touch` writes `last_seen_at` and nothing else, the same one-column rule
`GrantAdmin` carries in its doc comment: touch, extend and grant each own one
column so none can overwrite another's.

Down migration drops the column. The new binary's `GetLiveSession` selects
`last_seen_at`, so the down migration must run only after the binary is
rolled back, never before it or alongside it -- running it while the new
binary is still live fails every session lookup (sign-in, every
authenticated request) until the binary rollback finishes.

---

## 4. The formulas, pinned

Every figure the screen shows, defined once here so each implementer does not
invent one. `now` is the service clock, passed into every query so tests can
fix it.

| Figure | Definition |
|---|---|
| **Households** | `COUNT(*) FROM households` |
| **Active, last 7 days** | households with at least one session where `COALESCE(last_seen_at, created_at) >= now - 7 days`. Revoked sessions count: the activity happened before the revocation. |
| **Sign-ups requested, last 30 days** | `COUNT(*) FROM signups WHERE created_at >= now - 30 days`, either channel (`email` or `telegram_chat_id` set — the table's own CHECK guarantees exactly one). |
| **Sign-ups completed, last 30 days** | the same rows with `consumed_at IS NOT NULL` |
| **Invites pending** | `COUNT(*) FROM invites WHERE accepted_at IS NULL AND expires_at > now` |
| **Household last active** | `MAX(COALESCE(last_seen_at, created_at))` over that household's sessions; NULL when it has none, shown as "never" |
| **Member last active** | the same, over sessions matching both `user_id` and `household_id`; NULL shown as "never" |
| **Member count** | `COUNT(*) FROM memberships WHERE household_id = h.id` |
| **Channel** | `telegram` when a `telegram_accounts` row exists for the user, otherwise `email`. Derived from the join, never from `email IS NULL`: a user with neither is a bug the screen should surface, not a state it should name. |
| **Lockout** | `Policy.Evaluate(FailuresSince(householdID, now - Policy.Window), now)`; the drill-in shows `Until` when `Locked`, nothing otherwise. Same two calls `AuthService.SignIn` makes. |

**Search.** `q` is trimmed; empty means every household. Otherwise the
predicate, case-insensitive substring on each field:

```sql
h.name ILIKE pattern
OR h.family_name ILIKE pattern
OR EXISTS (
  SELECT 1 FROM memberships m JOIN users u ON u.id = m.user_id
  WHERE m.household_id = h.id
    AND (u.display_name ILIKE pattern OR u.email ILIKE pattern)
)
```

where `pattern` is `'%' || escaped || '%'` and `escaped` has `\`, `%` and `_`
escaped so a search for `_` does not match everything. `u.email` is `citext`,
so `ILIKE` on it is already case-insensitive; `ILIKE` is used on every field
anyway so nobody has to remember which columns are `citext`.

**Match.** When a row matched only through a member — neither `h.name` nor
`h.family_name` matched — the row carries `match: {memberName, memberEmail}`
for the first matching member by `joined_at`. When the household itself
matched, `match` is null. The operator sees why a row is there; the client
never re-runs the predicate.

**Ordering.** `lastActiveAt DESC NULLS LAST, createdAt DESC`. The household
someone is using right now is at the top; a household that has never been
used sorts below one that has, however old.

**Limit and truncation.** `limit` defaults to 50 and is clamped to 1…200 —
a non-integer, zero or negative value falls back to 50, anything above 200
becomes 200. The repository is asked for `limit + 1` rows; if it returns that
many, the service drops the last and sets `truncated: true`. This is how the
client knows there is more without a second `COUNT`.

**Time.** Every timestamp leaves the API as RFC 3339 in UTC. The browser
renders local time. Relative text ("3 days ago") is a display concern with
the exact instant in the element's `title`.

---

## 5. Service and ports

### 5.1 The port

In `usecase/ports.go`, next to the other admin ports:

```go
// AdminDirectoryRepository is the operator's read-only view across every
// household. It is the only port in the product that reads across
// household boundaries; everything else answers for one household.
// Nothing on it writes.
type AdminDirectoryRepository interface {
	// Metrics answers the four counters on the households page. The
	// cutoffs are passed in rather than computed here so the service's
	// clock is the only clock.
	Metrics(ctx context.Context, activeSince, signupsSince, now time.Time) (DirectoryMetrics, error)

	// SearchHouseholds returns up to limit households matching q (see the
	// spec's search predicate), ordered most recently active first. An
	// empty q matches every household. The caller passes limit+1 to learn
	// whether more exist.
	SearchHouseholds(ctx context.Context, q string, limit int, now time.Time) ([]HouseholdListing, error)

	// Household returns one household with its members and pending
	// invites. A missing household is domain.ErrNotFound.
	Household(ctx context.Context, householdID string, now time.Time) (HouseholdDetail, error)
}

type DirectoryMetrics struct {
	Households          int
	ActiveHouseholds    int
	SignupsRequested    int
	SignupsCompleted    int
	PendingInvites      int
}

type HouseholdListing struct {
	ID              string
	Name            string
	FamilyName      string
	MemberCount     int
	CreatedAt       time.Time
	LastActiveAt    *time.Time
	PrimaryCurrency string
	Match           *MemberMatch // nil when the household itself matched
}

type MemberMatch struct {
	Name  string
	Email *string
}

type HouseholdDetail struct {
	ID              string
	Name            string
	FamilyName      string
	CreatedAt       time.Time
	PrimaryCurrency string
	Members         []HouseholdMember
	PendingInvites  []PendingInvite
}

// MemberChannel is how a member signs in. It is set from the
// telegram_accounts join, never inferred from a NULL email.
type MemberChannel string

const (
	ChannelEmail    MemberChannel = "email"
	ChannelTelegram MemberChannel = "telegram"
)

type HouseholdMember struct {
	UserID       string
	Name         string
	Email        *string
	Channel      MemberChannel
	Role         domain.Role
	Capabilities domain.Capabilities
	LastActiveAt *time.Time
}

type PendingInvite struct {
	Name          string
	Email         string
	Role          domain.Role
	InvitedByName string
	ExpiresAt     time.Time
}
```

`SessionRepository` gains one method and `SessionRecord` one field:

```go
// LastSeenAt is nil until the first touch after migration 00013; readers
// treat nil as CreatedAt.
LastSeenAt *time.Time

// Touch records that the session was used at `at`. It writes one column,
// last_seen_at, and must not be combined with Extend or GrantAdmin: each
// owns its column so none can overwrite another's.
Touch(ctx context.Context, tokenHash []byte, at time.Time) error
```

### 5.2 The service

`usecase/admin_directory.go`:

```go
type AdminDirectoryDeps struct {
	Directory     AdminDirectoryRepository
	LoginAttempts LoginAttemptRepository
	Clock         Clock
	// Policy is the same lockout policy AuthService uses. Zero means
	// domain.DefaultLockoutPolicy, filled in by the constructor exactly as
	// NewAuthService does, so the two can never disagree by omission.
	Policy domain.LockoutPolicy
}

type AdminDirectoryService struct{ d AdminDirectoryDeps }

const (
	DirectoryDefaultLimit = 50
	DirectoryMaxLimit     = 200
	directoryActiveWindow = 7 * 24 * time.Hour
	directorySignupWindow = 30 * 24 * time.Hour
)

// Overview is the households page: the four counters and the matching
// households. q is trimmed; limit is clamped (see the spec's formulas).
func (s *AdminDirectoryService) Overview(ctx context.Context, q string, limit int) (DirectoryOverview, error)

// Household is the drill-in page: the household, its members, its pending
// invites and — when its password sign-in is currently locked — until when.
func (s *AdminDirectoryService) Household(ctx context.Context, householdID string) (HouseholdPage, error)

type DirectoryOverview struct {
	Metrics    DirectoryMetrics
	Households []HouseholdListing
	Truncated  bool
}

type HouseholdPage struct {
	HouseholdDetail
	LockedUntil *time.Time
}
```

The service holds the two window constants and the clamp rule; the repository
holds SQL and nothing about windows. `Household` calls the directory port
first and, only if that succeeds, `LoginAttempts.FailuresSince` — so an
unknown id is `ErrNotFound` before any second query runs.

Wiring in `cmd/api/main.go` follows `adminSvc`: construct
`postgres.NewAdminDirectoryRepo(db)`, then the service with the existing
`loginAttempts` repo and clock. `http.Deps` gains `AdminDirectory
*usecase.AdminDirectoryService`.

### 5.3 The repository

`postgres/queries/admin_directory.sql` — its own sqlc file, not appended to
`admin.sql`, because one file is one job and the flags SQL has nothing to do
with this — holds eight queries:

`CountHouseholds`, `CountActiveHouseholdsSince`, `CountSignupsSince` (one row,
two columns: requested and completed), `CountPendingInvites`,
`SearchHouseholds`, `GetHouseholdForAdmin`, `ListMembersForAdmin`,
`ListPendingInvitesForAdmin`.

`postgres/admin_directory_repo.go` implements the port over them. `Metrics`
runs its four counts in one transaction at `REPEATABLE READ` so the four
tiles describe one instant; the difference is invisible on this install and
free to have. `translate` turns `pgx.ErrNoRows` from `GetHouseholdForAdmin`
into `domain.ErrNotFound`, and nothing above the adapter ever sees a pgx
type.

---

## 6. API

Both routes sit inside the existing granted group in `router.go`, so
`requirePlatformAdmin`, `auditAdmin`, `requireCSRF` and `requireAdminGrant`
apply by construction and no new guard is written.

### `GET /api/v1/admin/households?q=&limit=`

```json
{
  "metrics": {
    "households": 3,
    "activeHouseholds7d": 2,
    "signups30d": { "requested": 9, "completed": 4 },
    "pendingInvites": 2
  },
  "households": [
    {
      "id": "6d1f…",
      "name": "Oentoro",
      "familyName": "Oentoro",
      "memberCount": 4,
      "createdAt": "2026-08-15T02:11:09Z",
      "lastActiveAt": "2026-09-02T07:40:12Z",
      "primaryCurrency": "SGD",
      "match": { "memberName": "Christine", "memberEmail": "c@example.org" }
    }
  ],
  "truncated": false
}
```

`lastActiveAt` and `match` are `null` when absent, never omitted, so the zod
schema on the client is exact. `match.memberEmail` is `null` for a
Telegram-only member.

### `GET /api/v1/admin/households/{householdID}`

```json
{
  "household": {
    "id": "6d1f…",
    "name": "Oentoro",
    "familyName": "Oentoro",
    "createdAt": "2026-08-15T02:11:09Z",
    "primaryCurrency": "SGD"
  },
  "members": [
    {
      "userId": "…",
      "name": "Andreas",
      "email": "andreas@example.org",
      "channel": "email",
      "role": "owner",
      "capabilities": ["calendar", "chores", "money", "marriage"],
      "lastActiveAt": "2026-09-02T07:40:12Z"
    },
    {
      "userId": "…",
      "name": "Kid",
      "email": null,
      "channel": "telegram",
      "role": "limited",
      "capabilities": ["calendar"],
      "lastActiveAt": null
    }
  ],
  "pendingInvites": [
    {
      "name": "Christine",
      "email": "c@example.org",
      "role": "owner",
      "invitedByName": "Andreas",
      "expiresAt": "2026-09-05T02:11:09Z"
    }
  ],
  "lockout": { "lockedUntil": "2026-09-02T08:02:00Z" }
}
```

`lockout` is `null` when the household is not locked.

**Status codes.** 200 with a body for both. 404 for an unknown household,
and also for a `householdID` that is not a UUID: the handler parses it
itself and answers 404 before calling the service, so the zero-UUID-to-500
path the first admin slice's handover names for flag overrides does not
recur here. Every other failure is 500, unhidden, as the surface's own rule
says.

**The DTO mapper fails closed.** `switch member.Channel` has `case
ChannelEmail`, `case ChannelTelegram` and a `default` that returns an error
— a 500 for a value nobody constructed, never an empty string in the JSON.

**Audit.** Each request writes one row, before the handler runs, as every
admin route does. With decision 5, `Detail` now carries
`{"query": "q=christine%40&limit=50"}` — the raw query string — whenever it
is non-empty, on every admin route. `auditAdmin`'s comment is updated to say
what `Detail` holds and why route parameters still are not in it.

---

## 7. Screens and states

Both pages are `React.lazy` under the existing `/admin` route, inside
`AdminShell`'s chrome and behind `AdminGate`, so a non-admin sees the
not-found page and an admin without a grant sees the password prompt, with
no new code for either.

### 7.1 Navigation

`AdminShell`'s header gains two links, "Flags" and "Households". The active
one is visibly distinct through a class the base styling does not override
— `docs/LEARNING.md`'s Frontend section records the audit branch shipping an
`activeProps` class that was concatenated onto the base and lost, invisible
to a jsdom test asserting `aria-current`. Bare `/admin` keeps redirecting to
`/admin/flags`.

### 7.2 The list, `/admin/households`

```
Hearth · Operator                    Flags   Households      Back to Hearth

Households

Search
[ christine@                          ] [ Search ]   Clear

┌ Households ┐ ┌ Active, 7 days ┐ ┌ Sign-ups, 30 days ┐ ┌ Invites pending ┐
│     3      │ │       2        │ │ 9 requested        │ │        2        │
│            │ │                │ │ 4 completed        │ │                 │
└────────────┘ └────────────────┘ └────────────────────┘ └─────────────────┘

Name        Family    Members  Created       Last active   Currency
─────────────────────────────────────────────────────────────────────
Oentoro     Oentoro   4        15 Aug 2026   today         SGD
  matched Christine · c@example.org
Tan         Tan       2        29 Aug 2026   3 days ago    SGD
Test        Smith     1        1 Sep 2026    never         USD

Showing 3 of 3
```

- **Search** is a `<form>`: label above the input (the product's own form
  rule), a submit button, Enter submits. Submitting navigates to
  `?q=<value>` — the URL is the state, so reload, back and the audit row all
  agree on what was shown. "Clear" is a link to the bare page. Nothing fires
  on keystroke.
- **Tiles** are four boxes with a hairline border on the canvas, no shadow.
  A large `tabular-nums` figure and a small muted label. The sign-ups tile
  carries two figures.
- **The table** uses hairlines between rows only. The name cell is the link
  to the drill-in and the row's accessible name. The `matched …` line appears
  only when `match` is non-null. Relative times carry the exact instant in
  `title`.
- **Below `md`** each row collapses to two lines — name and family, then
  members and last active — the same `flex-col sm:flex-row` move the flags
  page makes. No horizontal scroll at 320px.
- **Footer line.** "Showing N of N" when complete; a "Show more" button
  when `truncated` and the limit is under 200, which navigates with the limit
  doubled; "Showing the first 200 — search to narrow" at the cap.

States, each rendered and tested:

| State | What shows |
|---|---|
| Loading | Four tile skeletons and five row skeletons in the table's shape. No spinner. |
| Empty install | Tiles at zero and "No households yet." in place of the table. |
| No match | Tiles unchanged and "Nothing matches 'christine@'." with the Clear link. |
| Error, gate layer | `AdminGate` handles `ADMIN_REAUTH_REQUIRED`, 404 and lockout as it does for flags. |
| Error, other | The flags page's inline error block, above the tiles, with the message. |

### 7.3 The drill-in, `/admin/households/$householdId`

```
‹ Households

Oentoro
Family Oentoro · created 15 Aug 2026 · SGD · 4 members

Sign-in is locked until 16:02 (in 14 minutes).
Clear it early with adminctl unlock-household --email <owner>.

Members
Name        Channel                Role      Capabilities                     Last active
──────────────────────────────────────────────────────────────────────────────────────
Andreas     andreas@example.org    owner     calendar chores money marriage   today
Christine   c@example.org          owner     calendar chores money marriage   never
Kid         Telegram               limited   calendar                         30 Aug 2026

Pending invites
Christine   c@example.org   owner   invited by Andreas   expires 5 Sep 2026
```

- Back link, then the household name as the serif `h1` the flags page uses,
  then one summary line.
- **Lockout** is a `danger-soft` callout with `role="status"`, rendered only
  when `lockout` is non-null. It names the time and the command. Semantic
  colour for a real state, not decoration.
- **Members** is a table with the same hairline rule. Channel shows the email
  address, or the word "Telegram" — text, no icon. Role uses the household
  app's existing owner/limited badge tokens. "never" is muted.
- **Pending invites** lists each or says "None pending."
- Below `md`, both tables collapse to stacked rows.
- Unknown or malformed id: the API's 404 reaches `AdminGate`, which renders
  the not-found page — the same page a non-admin sees for the whole subtree.

### 7.4 Data layer

- `adminDirectorySchemas.ts`: zod schemas mirroring §6 exactly, `nullable()`
  where the API says null, `.strict()` so an unexpected key — a money field,
  say — fails the parse rather than passing through.
- `useAdminDirectory.ts`: `useAdminHouseholds(q, limit)` and
  `useAdminHousehold(id)`. Query keys include their arguments.
  `refetchOnWindowFocus: false` on both, with the same comment `useAdminFlags`
  carries: a refetch of an audited route is an audit row.
- Relative time: reuse the product's existing helper if one exists at
  implementation time; otherwise one small pure function in `lib/`, unit
  tested at the boundaries (just now, yesterday, days, months, never).

---

## 8. Error handling

| Where | Failure | Behaviour |
|---|---|---|
| Session middleware | `Touch` fails | `slog.Warn`, request continues. Next request retries. Never a 5xx for a usage timestamp. |
| Repository | no such household | `domain.ErrNotFound` at the adapter; 404 at HTTP. |
| Handler | `householdID` not a UUID | 404 before the service is called. |
| Handler | `limit` unparseable or out of range | clamped per §4, never a 400: the operator typed a URL, not a form. |
| DTO mapper | `Channel` is neither constant | error, 500. Fail closed on a value nobody constructed. |
| Anything else | repository error | 500, unhidden, as the surface's rule says. |
| Client | zod parse fails | the flags page's inline error, with the message. |
| Client | `ADMIN_REAUTH_REQUIRED`, 404, `ADMIN_LOCKED` | `AdminGate`, unchanged. |

---

## 9. Testing

Written before the code, per the project's TDD rule; each layer tests its own
contract against the layer below's double.

**Usecase, in-memory doubles:**
- `Overview` defaults `limit` to 50, clamps 0 and -1 to 50, clamps 500 to 200.
- `Truncated` is true only when the repository returns `limit + 1` rows, and
  the returned slice has `limit` entries.
- `q` is trimmed before it reaches the repository.
- `Household` returns `LockedUntil` when the policy evaluates the recorded
  failures as locked, and nil when it does not.
- `Household` on an unknown id returns `ErrNotFound` unchanged and never
  calls `FailuresSince`.

**Postgres, against testcontainers:**
- Search finds a household by name, by family name, by member display name
  and by member email, each case-insensitively; a search for `_` alone does
  not match every row.
- A Telegram-only member is found by name; the listing's `Match.Email` is
  nil; the detail's `Channel` is `ChannelTelegram` and `Email` nil.
- `Match` is nil when the household name matched, and names the member when
  only the member did.
- `CountSignupsSince` counts requested across both channels and completed
  only where `consumed_at` is set.
- Pending invites exclude accepted and expired rows.
- Active-7-days counts a household whose only session has an old
  `created_at` and a recent `last_seen_at`, and does not count one whose
  session has neither within the window.
- Ordering: most recently active first, never-active last.
- `Touch` changes `last_seen_at` and leaves `expires_at` and
  `admin_grant_expires_at` untouched.
- `Household` on an unknown id is `ErrNotFound`.

**HTTP:**
- Both routes join the existing table-driven admin tests
  (`TestAdminRoutesAre404ToANonAdmin`, `TestAdminRoutesNeedAGrant`,
  `TestEveryAdminRequestIsAudited`), so 404-to-non-admin, grant-required and
  one-audit-row-per-request are asserted by the same tests as every other
  admin route.
- The audit row for a search carries the query string in `Detail`.
- A malformed `householdID` answers 404.
- The response key sets for both routes are asserted exactly, so no money
  key can appear.
- The session middleware touches a session whose `LastSeenAt` is nil, and
  one older than an hour, and does not touch one ten minutes old; a failing
  `Touch` still answers 200.

**Frontend:**
- Both schemas reject a payload with an extra key and one with a missing
  nullable field.
- The list page renders loading, empty, no-match, truncated-with-more and
  truncated-at-cap.
- Submitting the search form navigates with `q`; typing does not fetch.
- The drill-in renders the lockout callout only when `lockout` is non-null,
  and shows "Telegram" for that channel.
- `adminBundleSplit.test.ts` pins both new pages out of the main bundle.

**Mutation checks — at least these three, each seen red before green, per
the `proving-tests-can-fail` skill:**
1. Change the active-7-days cutoff to read `created_at` alone: the
   old-session-recently-touched test must fail.
2. Drop `u.email` from the search predicate: the member-email test must
   fail.
3. Remove the touch throttle so every request touches: the
   ten-minutes-old-is-not-touched test must fail.

**The browser walk**, fifteen criteria, recorded in its own verification
file when run. Seed: the design's household plus one Telegram-linked
member, one pending invite, and a second household with no sessions.

1. Signed in as a non-admin, `/admin/households` shows the not-found page,
   indistinguishable from a typo.
2. As the platform admin, the re-auth prompt appears, the correct password
   opens the list, and the four tiles render.
3. Each tile equals the matching `psql` count, run in the same minute.
4. The header shows Flags and Households, and the active link is visibly
   distinct — checked by eye, not only in the accessibility tree.
5. Searching a member's email finds their household and the row names the
   member.
6. Searching a household name in the wrong case finds it.
7. Searching nonsense shows "Nothing matches" and Clear restores the list.
8. One search is one request in the network panel and one new
   `admin_audit_log` row, whose `detail` holds the query string.
9. The Telegram-only member is found by name; the drill-in shows "Telegram"
   and no email.
10. The drill-in's members table shows roles, capabilities and last active;
    the never-signed-in member shows "never".
11. The pending invite is listed with inviter and expiry; accepting it in
    another browser removes it and the member count rises by one on reload.
12. Three wrong passwords on that household's sign-in make the lockout
    callout appear with a time; `adminctl unlock-household` makes it
    disappear on reload.
13. A member's request in a fresh session sets `last_seen_at` in `psql`; a
    second request a minute later does not change it.
14. At 320px, list rows collapse to two lines and neither page scrolls
    horizontally.
15. A URL with an unknown UUID, and one with `not-a-uuid`, both show the
    not-found page.

---

## 10. Out of scope

- **Creating a flag override from the drill-in** (decision 1). The section
  it will occupy is beneath Pending invites.
- **Any write to a household.** See §1.
- **Members total, sign-ups by channel, per-day charts.** Counters, not
  analytics; the tiles are what the operator acts on.
- **Search by Telegram chat id or username.** The product stores only the
  numeric `chat_id`, which the operator will not have.
- **A per-household audit trail** ("what did the operator look at about this
  household"). The audit log already holds it; a UI for the log was built and
  descoped on 2026-09-02.
- **Pagination beyond the 200 cap.** Search is the tool past that point. A
  real page cursor is a small addition if a list ever needs it.

---

## 11. Documentation owed

Updated in the same change as the code, not afterwards:

- `docs/SYSTEM_DESIGN.md` — the two routes and their guards, the
  `sessions.last_seen_at` column and the touch, the new service, port and
  repository. Use the `maintaining-system-design` skill.
- `docs/FEATURE_TRACKER.md` §9 — "Households and metrics" ⬜ → ✅. "Admin
  flags screen" stays 🟡 with its cell reworded: the household list now
  exists, the override control still does not. Recount §9 by symbol
  (5/1/2/1) and the totals (78/17/22/3 = 120; 95 of 120 built or partly
  built).
- `docs/LEARNING.md` — whatever the work teaches. Decision 10 goes in
  regardless, under pattern 8 (configuration that lies): a counter window
  and a prune retention that can silently disagree.
- `docs/superpowers/specs/2026-09-01-hearth-admin-surface-design.md` — §6
  points here; §9's "each of 4, 5 and 6 sits behind its own configuration"
  corrected to name 4 and 5 only.
- `docs/ADMIN_SURFACE_HANDOVER.md` §3 and `docs/HANDOVER.md` §4 — households
  moves from "does not exist" to built; the next item is the message
  inspector.
- `deploy/README.md` — one line beside the prune command: `--older-than`
  must stay at 30 or above, or the sign-ups tile under-counts.
- `docs/GUIDE.md` — nothing. The operator surface is documented in the
  handover, not the household guide.

---

## 12. Rollout

1. Migration `00013_session_last_seen.sql`. Nullable column, no backfill,
   and the old binary ignores it -- but the new binary's `GetLiveSession`
   selects `last_seen_at` unconditionally, so this migration must go out
   ahead of the binary, never behind it. `docker-compose.prod.yml` already
   makes `api` depend on `migrate` completing first, so a normal deploy
   through `deploy/deploy.sh` enforces the order automatically.
2. The binary: touch, port, service, repository, routes, pages, in one
   deploy through `deploy/deploy.sh <sha>` as usual.
3. No configuration, no provisioning, no new secret. Nothing in
   `docs/INFRASTRUCTURE.md` changes.

Rolling back is `deploy.sh --rollback` first, then the down migration --
never the reverse. `GetLiveSession` on the new binary selects `last_seen_at`,
so dropping the column while that binary is still serving fails every
session lookup until the binary rollback catches up.

---

## 13. Definition of done

`make lint && make test` green, the three mutation checks in §9 seen red,
`docs/FEATURE_TRACKER.md` and `docs/LEARNING.md` updated, and
`docs/SYSTEM_DESIGN.md` kept true — this slice adds two routes, a column, a
port, a service and a repository, every one of which that document draws.

Then the fifteen-criterion browser walk in §9, the same bar every Money
feature, Retros, Vision and the first admin slice were held to, recorded in
its own verification file. A feature is not done because its tests pass.
