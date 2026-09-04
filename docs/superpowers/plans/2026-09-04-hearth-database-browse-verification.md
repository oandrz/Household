# Hearth — read-only database browse, browser verification

Walked 2026-09-04 (the walk ran past local midnight into 2026-09-05; the date
in this heading is the day it started and the day the tracker row cites)
against the running dev stack (`hearth-api-1`, `hearth-web-1`,
`hearth-postgres-1`, `hearth-mailpit-1`) at <http://localhost:5173>, on branch
`admin-db-browse`.

**Browser tool: Playwright MCP for the entire walk.** Claude in Chrome was not
attempted at all, which is a departure from earlier walks and a deliberate
one: the previous verification file on this same box
(`2026-09-04-hearth-outbound-inspector-verification.md`) documents at length
that Chrome's `computer` screenshot capture here silently drops all but the
first item of a flex row at this display's `devicePixelRatio: 2.5`. This
walk's single highest-value criterion (15) is *precisely* a flex-row nav at
305px, so the one tool already known to lie about that exact thing was left
alone rather than re-proven to lie. Every navigation, click, `evaluate`,
screenshot and viewport change below is Playwright.

**Result: 15 of 15 criteria pass.** One real defect was found and fixed during
the walk — not a behaviour defect but a **prose** one, in the two comments
that exist to explain what `«null»` means. Both cited an example the feature
makes impossible to observe. Details under criterion 7 and in "The defect"
below. Nothing about the running behaviour was wrong.

**Five things were met by an interpreted rather than literal path**, each
labelled where it occurs so the count is honest rather than flattering. Two
are substitutions the brief's own text made necessary, two are methods, and
one is setup:

| Where | Interpreted how |
|---|---|
| Criterion 7 | On `goal_contributions` and `users.email`, because the brief's `users.password_hash` is redacted and can never render `«null»` |
| Criterion 9 | No-repeat proof on a static `categories?limit=5`; the write-between-pages case a browser cannot stage is cited to the repository test that covers it |
| Criterion 2 | Timings from the Performance Timeline, not devtools' Network tab |
| Criterion 6 | Response body read with `fetch(...).text()`, not devtools' Network tab |
| Setup | Stack brought up detached, not through a foreground `make dev` |

**One claim this file made and had to retract.** It originally said the brief
was wrong about criterion 13's SQL as well. It was not; the retraction and the
three `psql` runs that settle it are under criterion 13.

---

## How the stack was brought up, and what was not run

- `docker compose down`, then `docker compose up -d --build postgres mailpit
  api web`. **Not literally `make dev`**, which tails logs in the foreground
  and cannot be driven from a non-interactive session; `make up` is the same
  four services with the same build, detached, and the `migrate` and
  `readonly-role` one-shots still ran (both `Exited` cleanly, visible in the
  compose output) because `api`'s `depends_on` requires them. Recorded as an
  interpreted rather than literal path.
- **`lsof -i :5173` before trusting localhost**, as the brief required. It
  showed a single `ssh` listener (pid 10610) — that is colima's port
  forwarder, not Docker Desktop, and `docker context ls` confirms `colima *`
  is the active context. Nothing was listening on 5173 before `up`; the
  vite service really was absent, as the brief said. Both engines exist on
  this box; only colima served this walk.
- **`make seed` was NOT run.** The brief permits it "if a criterion needs
  it"; none did. The box already carried 6 users, 2 households, 13
  categories and 160 audit rows. Running it would have produced an
  already-exists path to explain rather than data to read.
- **Sign-in was not exercised.** An existing session cookie for
  `andreas@hearth.family` survived on the Playwright profile, so the walk
  opened straight onto the Overview. The **re-authentication was** exercised
  in full (criterion 4) and `hearth-dev-password` was accepted on the first
  attempt — no `make reset-password` was needed this time, unlike the
  previous walk. The account's 3-try lockout was never approached.

---

## One thing was created through the product, on purpose

`accounts` was **empty** (0 rows), and criteria 3 and 4 are both about reading
a household's real money out of that table. Rather than hand-insert a row with
`psql` — which would have proven the browse can display a row this walk had
just written, not a row the product wrote — **one account was created through
the product UI**, at `/money` → "+ Add account":

> `DBS Joint Savings`, type Cash & savings, owner Shared, starting balance
> **12345.67 SGD**, as-of 2026-09-04, counting toward net worth.

It is left in place on the dev box. It is genuine product output, and it gave
the walk an unusually good cross-check: the same value reads `S$12,345.67` on
`/money` and `1234567` in the browse's `opening_balance_minor` column, and
those two readings were taken *minutes apart with the browse switched off in
between* (criterion 14). Money as `int64` minor units, seen end to end.

---

## The criteria

### 1. The nav shows four links, Database active on `/admin/database`

**Done:** Opened `/admin`, which settled on `/admin/flags` behind the re-auth
prompt; entered the password; then clicked **Database** in the operator nav
(not a typed URL — the link itself is what the criterion is about). Read
`aria-current` and computed styles off every `<a>` in `nav[aria-label="Operator"]`.

**Seen:** Five links, left to right — `Flags`, `Mail`, `Households`,
**`Database`**, `Back to Hearth`. The four the criterion names are there and
in `AdminShell.tsx`'s `items` order.

| Link | `aria-current` | `color` | `font-weight` |
|---|---|---|---|
| Flags | `null` | `oklab(… / 0.6)` | 500 |
| Mail | `null` | `oklab(… / 0.6)` | 500 |
| Households | `null` | `oklab(… / 0.6)` | 500 |
| **Database** | **`"page"`** | **`rgb(255, 255, 255)`** | **600** |
| Back to Hearth | `null` | `oklab(… / 0.7)` | 500 |

Two independent signals separate the active link — full opacity and weight
600 — which is the same pattern the households and mail walks recorded, so the
fourth link did not weaken it.

**Result: PASS.**

---

### 2. The table list renders every table with a row count, and a timing baseline

**Done:** Read all 34 rows off `ul[aria-label="Tables"]`, then cross-checked
**every one** against `psql` (a `DO` block running `count(*)` per table in
`information_schema`) rather than spot-checking one, since the comparison was
free once written.

**Seen:** 34 tables. `admin_audit_log` (164 rows · 7 columns) and
`goose_db_version` (14 rows · 4 columns) are both present, as the criterion
requires — the browse hides neither its own audit trail nor the migration
bookkeeping. **All 34 counts matched `psql` exactly, table for table**, and
the singular/plural label is right at both ends (`1 row`, `0 rows`).

The blurb above the list reads: "Read-only, one page at a time. Every table
opened here is an audit row, and columns holding a secret are never selected
at all — a table's own page explains the markers that stand in for them."

**Wall-clock baseline for decision 11** (`GET /api/v1/admin/db/tables`, which
counts every table in one statement under a 3-second cap):

| Sample | ms |
|---|---|
| Cold, first paint (from `performance.getEntriesByType('resource')`) | **41.2** |
| Warm ×5 (in-page `fetch`, timed with `performance.now()`) | 8.0, 4.9, 4.7, 4.6, **4.2** |

Against the 3000 ms `statement_timeout` that is **1.4 % cold and ~0.2 % warm**
— roughly a 70× margin cold, 400× warm, over a schema of 34 tables and about
660 rows in total. Transfer size 16.8 KB. Decision 11's "revisit when it times
out" clause now has a number to be revisited against instead of a guess.

**Interpreted rather than literal, and labelled as such:** the brief says to
record the wall-clock time "from devtools' Network tab". These figures come
from the Performance Timeline instead — `performance.getEntriesByType('resource')`
for the cold sample, which is the browser's own timing for the request the
page itself made and is the same number the Network panel displays, and
`performance.now()` around five in-page `fetch`es for the warm ones. The warm
samples are additionally *not* what the panel would show for a page load: they
are five extra requests issued deliberately to get a stable figure, and each
one left its own audit row.

**Result: PASS.**

---

### 3. Opening `accounts` shows its columns and its rows, matching `psql`

**Done:** Clicked the `accounts` link from the list (a real SPA navigation,
which is also what criterion 12 counts). Compared every rendered cell against
`select … from accounts` in `psql`.

**Seen:** All 12 columns in the left pane with their types, all 12 as grid
headers, and the single row. Cell by cell against `psql`:

| Column | On screen | `psql` |
|---|---|---|
| `id` | `d4f9ae58-8bfc-4acc-9e1b-a609401bff51` | same |
| `household_id` | `96096965-af91-4a39-b299-320f5324f464` | same |
| `nickname` | `DBS Joint Savings` | same |
| `type` | `cash` | same |
| `owner_membership_id` | `«null»` | NULL |
| `opening_balance_minor` | `1234567` | same |
| `opening_balance_currency` | `SGD` | same |
| `opening_balance_as_of` | `2026-09-04` | same |
| `count_toward_net_worth` | `true` | `t` |
| `visible_to_limited_members` | `false` | `f` |
| `archived_at` | `«null»` | NULL |
| `created_at` | `2026-09-04 15:50:54.521612+00` | same |

**One difference worth naming rather than glossing:** booleans read `true` /
`false` on screen where `psql` prints `t` / `f`. That is the browse's `::text`
cast, not a discrepancy — Postgres's own text output for `boolean` is
`true`/`false`, and `psql` is the one applying a display shorthand. The
figures the criterion is about (the UUIDs, the minor-unit integer, the date)
match verbatim.

**Result: PASS.**

---

### 4. A money column is readable, and getting there cost a re-authentication

**Seen:** `opening_balance_minor` reads **`1234567`** with
`opening_balance_currency` **`SGD`** beside it — S$12,345.67, the exact figure
typed into the product's own Add-account form minutes earlier, for the
household `96096965-…` (Andreas & Christine). A second money column was read
the same way on `goal_contributions`: `amount_minor` **`50000`** (S$500.00).

**And it cost a re-authentication.** Reaching this screen required entering
`hearth-dev-password` at "Confirm your password / Re-enter your password to
open the admin surface" — that prompt is what stood between an already
signed-in platform admin and every household's finances, and it was not
skippable. This is the whole point and the whole cost, and both halves were
observed.

**Result: PASS.**

---

### 5. `sessions` renders `token_hash` as `«redacted»`, and the header says so

**Seen:** On `/admin/database/sessions`, all **28** rows render `token_hash`
as `«redacted»` — no exceptions, no partial values. The column is marked in
**both** places it appears:

- Grid header: `token_hash` with `redacted` on a second line beneath it.
- Columns pane: `token_hash` / `bytea · redacted`.

Both use the single word `redacted`, matching the marker itself — the wording
commit 47abd1f ("one word for a withheld column, not three") settled.

**Result: PASS.**

---

### 6. The redaction is real, not cosmetic

**Done:** Read the **raw response body** for
`GET /api/v1/admin/db/tables/sessions?limit=50&offset=0` — `fetch(...).text()`
from the page's own context, which is the same bytes the Network panel shows
(and the same request Playwright's network log records as `[200] OK`).
Searched it for every shape a `bytea` could have leaked in.

**Interpreted rather than literal, and labelled as such:** the brief says to
read the body in devtools' Network tab. `fetch(...).text()` from the page's
own context returns the identical bytes — the response is read before any
JSON parsing or React rendering can touch it — and unlike squinting at a
panel it can be searched exhaustively, which is what the checks below do. The
same request appears in Playwright's own network log, so the page really did
issue it. The property the criterion is about holds either way; the *method*
is not the one the brief names.

**Seen** (7866 bytes):

| Check | Result |
|---|---|
| `distinctTokenValues` across all 28 rows | `["«redacted»"]` — one value, the marker |
| Body contains `\x` (Postgres bytea prefix) | **false** |
| Hex runs of 16+ chars (excluding UUIDs) | **none** |
| Base64-ish runs of 40+ chars | **none** |
| `«redacted»` occurrences | 28, exactly one per row |
| `token_hash` column metadata | `{"name":"token_hash","dataType":"bytea","redacted":true}` |

The secret bytes are not in the response at all. They are never fetched: the
marker is a bind parameter substituted **inside the `SELECT` list**, so
Postgres never hands them to Go. This is the feature specified, not a marker
painted over a value that was still sent.

**Class sweep, beyond the criterion.** A redaction that is right on `sessions`
and wrong elsewhere would still be a leak, so the `redacted` flag the running
API reports was checked against the **whole schema** — all 241 columns of all
34 tables:

- **6 columns redacted:** `invites.token_hash`, `magic_links.token_hash`,
  `sessions.token_hash`, `signups.token_hash`,
  `telegram_link_requests.nonce_hash` (all `bytea`, caught by the type rule)
  and `users.password_hash` (`text`, caught by the `_hash` name rule — the
  case the type rule exists to admit it cannot cover).
- **0 `bytea` columns unflagged.** The type rule has no gaps in this schema.
- **3 columns matched a broad secret-ish name pattern and are correctly NOT
  redacted:** `feature_flags.key`, `household_feature_flags.key`,
  `spaces.key`. All three hold flag identifiers (`signups_open`,
  `telegram_sign_in`), not secrets — false positives of the word "key", and
  redacting them would make the flags screen unreadable for no gain.

No leak found, and no denylist entry needed. The empty `redactedColumns` map
stays empty, which is what its own comment asks for.

**Result: PASS.**

---

### 7. A `NULL` renders `«null»`, distinguishable from an empty string

**This is the criterion that was met by an interpreted path, and it is where
the walk's one defect was found.**

**The brief's suggested column cannot satisfy this criterion.**
`users.password_hash` **is** NULL for a magic-link-only member — confirmed in
`psql`: Kayla (`b263a74f-…`) and Ethan (`c2751e2d-…`) both have
`password_hash IS NULL`. But `ColumnIsRedacted` matches its `_hash` suffix, so
the browse substitutes the marker in the `SELECT` list **unconditionally** and
never fetches the value. Confirmed on screen at `/admin/database/users`: both
members render **`«redacted»`**, not `«null»`. A NULL in a redacted column can
never show `«null»`, by construction.

So the criterion was met on two other tables, with real seeded data and no
hand-inserted rows:

**`goal_contributions` — both markers on one row, which is the strongest form
of the criterion:**

| Column | Type | Rendered | Length |
|---|---|---|---|
| `note` | `text NOT NULL`, value `''` | *(empty cell)* | **0** |
| `source_budget_month` | `date`, NULL | **`«null»`** | 6 |

An empty string and a NULL, side by side in the same row, rendering
differently. That is exactly the confusion `NullCell` exists to prevent, shown
rather than argued.

**`users.email` — the honest magic-link example.** It is NULL for Kayla and
Ethan (the household's children, created with no email) and renders
**`«null»`**, while the four adult members show real addresses. This is the
column the comments *should* have named: nullable for exactly the members they
describe, and not redacted.

**Result: PASS**, via `goal_contributions` and `users.email` rather than the
brief's `users.password_hash`. Recorded as interpreted, and the reason the
brief was wrong is written down below rather than left for the next walker to
rediscover.

---

### 8. The legend explains both markers, readable without hovering

**Seen:** Directly above the grid, as ordinary body text:

> `«redacted»` is a value you may not see. `«null»` is a value that is not
> there.

Measured rather than eyeballed: `display: block`, `opacity: 1`, `font-size:
12px`, `color: rgba(0, 0, 0, 0.55)`, laid out at y=172 above the grid, and
**no `title` attribute** on the element — there is nothing to hover, because
nothing is hidden behind a hover. Present on every table's page, including
tables with no redacted column at all (`accounts`, `categories`), which is
correct for a fixed legend: it explains the vocabulary of the screen, not the
contents of one table.

**Result: PASS.**

---

### 9. Paging works, does not repeat a row, and the URL carries the offset

**Done, two ways, because no single table proves both halves cleanly.**

**(a) `categories?limit=5` — three pages, static table, no repeats.** Chosen
because it is the only multi-page case on this box that nothing writes to
during the walk. Clicked Next twice, collecting every `id`:

| Page | URL | Pager | ids |
|---|---|---|---|
| 1 | `?limit=5&offset=0` | Rows 1–5 of 13 | `038f…`, `1ce0…`, `230f…`, `7466…`, `785c…` |
| 2 | `?limit=5&offset=5` | Rows 6–10 of 13 | `7938…`, `8c2c…`, `b384…`, `b47f…`, `b9a0…` |
| 3 | `?limit=5&offset=10` | Rows 11–13 of 13 | `c8c2…`, `da0b…`, `db06…` |

**13 ids, all distinct, strictly ascending across page boundaries** — the
primary-key `ORDER BY` doing its job. None repeated, none skipped, and the
count matches `psql`'s 13 exactly. The *ascending* part is the load-bearing
half rather than merely "no duplicates": `categories.id` is
`gen_random_uuid()`, so key order is uncorrelated with heap order, and three
pages arriving in strict key order is positive evidence that an `ORDER BY` was
applied — not just that nothing happened to be returned twice.

**What a browser cannot prove here, and where it is proven instead.** The
failure this `ORDER BY` actually exists to prevent is a **write between two
pages**: on a table that is only read, one connection at a time, an unordered
`LIMIT/OFFSET` returns the same heap order to every page and the defect stays
invisible at any row count. Reproducing that from a browser means racing a
write against two clicks, which this walk did not attempt. It is covered at
the repository level by `TestRowsPagesWithoutRepeatingOrSkipping`
(`api/internal/adapter/postgres/browse_repo_test.go:160`), whose own comment
says the write between the pages "is the point, and it is why this read-only
test writes at all". Recorded as the one gap in this criterion that the layer
below closes rather than this one.

**(b) `admin_audit_log` at the default limit — the >50-rows case.**
`?limit=50&offset=100` rendered exactly 50 rows, "Rows 101–150 of 196".

**The URL is the state, proven both directions:**

- **Reload (F5)** on `?limit=5&offset=10` came back on the same three ids and
  the same "Rows 11–13 of 13" — and did **not** re-prompt for the password,
  which is right: the admin grant lives on the session row, not in memory.
- **Back** from there landed on `?limit=5&offset=5` showing page 2's five ids.

URL, request and screen agree, which is the property the audit row depends on
to mean anything.

**Result: PASS.**

**Observation, not a defect:** paging `admin_audit_log` changes
`admin_audit_log` — its total ticked 164 → 196 → 198 across this walk, because
every page view of it appends a row to it. The `ORDER BY` is a random UUID, so
a row inserted mid-paging lands in an arbitrary position. Within a single read
the ordering is stable and correct; across two clicks a growing table can
shift. That is inherent to paging a live append-only table by primary key, not
something this feature got wrong, and it is precisely why criterion 9's clean
no-repeat proof was taken on a static table instead.

---

### 10. Previous disabled on the first page, Next on the last

**Seen**, on the `categories?limit=5` sequence above:

| Page | Previous | Next |
|---|---|---|
| 1 (offset 0) | **disabled** | enabled |
| 2 (offset 5) | enabled | enabled |
| 3 (offset 10, last) | enabled | **disabled** |

Read off the `disabled` DOM property, not from styling. Note the last page
holds 3 rows against a limit of 5, and Next is still correctly disabled —
`atEnd` is computed from the rows actually returned (`offset + rows.length >=
total`), not from the limit, which is the one that is true on a short final
page.

**Result: PASS.**

---

### 11. An unknown table says so, and the operator surface stays open

**Done:** Navigated directly to `/admin/database/nope`.

**Seen:** The route normalised to `?limit=50&offset=0` and rendered:

> ‹ Database
>
> There is no table called `nope` in this database — check the spelling
> against the list, or the read-only role may not be granted on it.

The operator header is intact, all four nav links present, `Database` still
the active one, and a working ‹ Database link back to the list. **No password
prompt appeared** — which is the half of this criterion that is really about
`useCloseSurfaceOnReauth` not firing on a 404, the trap
`AdminDatabasePage.tsx`'s longest comment is about.

**Result: PASS.**

**Beyond the script — the injection surface.** This route is the one place a
string from a URL reaches SQL construction, and the repo's own comment claims
the name "has still not touched a query except as a bind parameter". Six
probes, all through the API:

| Probe | Answer |
|---|---|
| `users'; DROP TABLE users; --` | `404 NOT_FOUND` |
| `users" OR "1"="1` | `404 NOT_FOUND` |
| `pg_shadow` | `404 NOT_FOUND` |
| `information_schema.columns` | `404 NOT_FOUND` |
| `USERS` | `404 NOT_FOUND` |
| `goose_db_version` | `200 OK` |

All refusals carry the same generic `{"error":{"code":"NOT_FOUND","message":"That
could not be found."}}` — no probe learns anything the others do not.
`pg_shadow` and `information_schema.columns` are refused because the lookup is
scoped to `public` `BASE TABLE`s. `USERS` 404ing is **correct, not a bug**:
`information_schema` matching is exact, and a case-insensitive match here would
be a second, weaker rule than the one the `SELECT` actually uses. Afterwards,
`users` still holds its 6 rows and `to_regclass('public.users')` is non-null —
nothing was dropped.

**Also beyond the script — two malformed-input cases a first-time operator
would hit.** Both are handled by two layers that agree:

- `?limit=500` → **the URL itself is rewritten to `?limit=100`** by the
  route's `validateSearch`, and the page shows 100 rows of 198. So the URL,
  the screen and the audit row all say 100; there is no state where the URL
  claims a limit the server silently clamped.
- `?offset=-1` → the URL is rewritten to `?offset=0` and the first page
  renders, with **no red alert** — a coerced value, not an error thrown at
  someone who mistyped a URL.
- Behind that clamp the API's own guard is still strict: `offset=-1`,
  `limit=0`, `limit=-5`, `limit=abc` and `offset=abc` each answer
  `400 INVALID_RANGE` with "limit must be a positive whole number and offset
  must not be negative."

---

### 12. Every page view leaves exactly one audit row, naming the table and the offset

**Done:** Settled on `/admin/database`, counted, then made **one** page view —
a single SPA click into `memberships`, which issues exactly one API request —
then counted again.

**Seen:**

```
BEFORE: 193
AFTER:  194
```

**Delta exactly 1.** The newest row, in full:

```
id            | 467a4ef7-c179-4979-98c2-ff7443a995dd
actor_user_id | 46c82775-774a-4333-9164-9fd4770477cc   (andreas@hearth.family)
action        | GET /api/v1/admin/db/tables/memberships
target        | /api/v1/admin/db/tables/memberships
detail        | {"query": "limit=50&offset=0"}
ip            | 172.19.0.5:37438
created_at    | 2026-09-04 15:55:37.826032+00
```

It names **the table** (`memberships`, in both `action` and `target`) and
**the offset** (`detail.query`). Confirmed with a non-zero offset too: a view
of `admin_audit_log?limit=50&offset=100` wrote
`detail | {"query": "limit=50&offset=100"}`.

**A note for the next walker, because this walk briefly got it wrong.** The
first read of the row selected named columns and omitted `detail`, which made
it look as though the offset was not recorded at all — a false alarm that took
a detour through `auditAdmin` to clear up. **Select `*`.** The offset is never
in `target`; `auditAdmin` runs before chi matches the route, so the path is
all it can put there, and the query string goes to `detail` deliberately.

**Result: PASS.**

---

### 13. The role cannot write

Not a click. Run against the real `hearth_readonly` role, and pushed past the
one statement the criterion asks for, because `deploy/readonly-role.sql`
claims the guard is "read-only twice over ... the two fail independently" and
a walk that tests one guard cannot tell whether the second exists.

**The exact refusals:**

```
$ docker compose exec postgres psql -U hearth_readonly -d hearth \
    -c "insert into households (id, name, family_name) values (gen_random_uuid(), 'nope', 'nope')"
ERROR:  cannot execute INSERT in a read-only transaction
```

That is **guard 1**, `ALTER ROLE hearth_readonly SET default_transaction_read_only
= on`. Postgres checks the transaction's read-only setting before it checks
table privileges, which is why this message and not "permission denied" —
exactly what `deploy/PROVISION.md` section 9 was corrected to expect in commit
c993059.

Guard 1 is one statement away from being switched off by the session itself,
so the walk switched it off:

```
$ psql -U hearth_readonly -d hearth \
    -c "BEGIN; SET TRANSACTION READ WRITE; insert into households (id, name, family_name) values (gen_random_uuid(), 'nope', 'nope');"
BEGIN
SET
ERROR:  permission denied for table households
```

That is **guard 2**, `GRANT SELECT` and nothing more. With guard 1 deliberately
bypassed, the write still fails. The two really are independent, and the
claim in the SQL file is true.

Three more shapes, all refused by guard 1:

```
ERROR:  cannot execute UPDATE in a read-only transaction        (update users set display_name='pwned')
ERROR:  cannot execute DELETE in a read-only transaction        (delete from sessions)
ERROR:  cannot execute DROP TABLE in a read-only transaction    (drop table if exists households cascade)
ERROR:  cannot execute CREATE TABLE in a read-only transaction  (create table evil (id int))
```

Afterwards: `households` = 2, `sessions` = 28, users named `pwned` = 0.
Nothing was written, updated, deleted or dropped.

**A correction this file made about the brief, and then had to retract —
recorded rather than quietly deleted, because the retraction is the more
useful half.**

This section originally claimed that the task brief's two-column SQL,
`insert into households (id, name) values (…, 'nope')`, was not runnable:
`households.family_name` is `NOT NULL` with no default, so — the claim went —
Postgres would answer a `NOT NULL` violation and the pass would prove nothing
about the role. The three-column form (the parent prompt's) was run instead.

**That claim is false, and it was never checked before being written down.**
Review caught it; the brief's exact statement was then run as
`hearth_readonly` against this same stack:

```
$ psql -U hearth_readonly -d hearth -c "insert into households (id, name) values (gen_random_uuid(), 'nope')"
ERROR:  cannot execute INSERT in a read-only transaction
```

It produces the criterion's own refusal. `households` was still 2 afterwards.
The brief's SQL was fine as written and needed no correction at all.

**Why, checked three ways rather than asserted.** Both of this role's guards
run in `ExecutorStart` — `PreventCommandIfReadOnly` for
`default_transaction_read_only` and `ExecCheckPermissions` for the `GRANT` —
while `NOT NULL` is enforced by `ExecConstraints` during `ExecutorRun`. The
statement is refused before a row is ever built, so the missing column is
never reached. Run as the *owner*, where neither guard applies, the same
statement does exactly what the retracted claim predicted:

```
$ psql -U hearth -d hearth -c "insert into households (id, name) values (gen_random_uuid(), 'nope')"
ERROR:  null value in column "family_name" of relation "households" violates not-null constraint
```

And guard 2 alone also precedes it — bypassing only the read-only guard, with
the two-column form still missing `family_name`:

```
$ psql -U hearth_readonly -d hearth -c "BEGIN; SET TRANSACTION READ WRITE; insert into households (id, name) values (gen_random_uuid(), 'nope');"
ERROR:  permission denied for table households
```

So the brief's two-column statement proves **both** guards independently,
exactly as the three-column form this walk actually ran does.

**The conflation that produced the false claim**, which is the part worth
keeping: an *unknown column name* really does fail before the guards, because
it fails at parse analysis, before the executor starts at all —

```
$ psql -U hearth_readonly -d hearth -c "insert into households (id, nosuchcolumn) values (gen_random_uuid(), 'nope')"
ERROR:  column "nosuchcolumn" of relation "households" does not exist
```

— but an *omitted* NOT NULL column is a runtime check that the guards never
let the statement reach. Two different failures at two different stages, and
this file collapsed them into one. **The criterion's own pass is unaffected**:
the three-column form that was run is a valid write attempt, and guard 2 was
proven independently by the bypass above.

**Result: PASS.**

---

### 14. The not-configured state — both of them

The design changed during the build and this is now **two** states with two
messages sending the operator to two different places. Both were walked. Both
used a Compose override file in the scratchpad plus `docker compose -f
docker-compose.yml -f <override> up -d api`, so the container keeps its name
(`hearth-api-1`) and `web`'s proxy to `api:8080` keeps resolving — `.env` was
not touched and `docker-compose.yml` is confirmed unchanged (`git diff
--exit-code`, exit 0).

**State A — `DATABASE_READONLY_URL` unset** (empty string; `BrowseEnabled()`
is `!= ""`, so empty and unset are the same state).

Startup log: the `database browse enabled` line is **absent**, and the API
otherwise boots normally.

On screen at `/admin/database`, the heading and blurb still render, the
operator nav is fully intact, no password prompt, and one alert:

> The database browse is switched off on this install. Set
> DATABASE_READONLY_URL and restart the API.

Both routes answer:

```
503 {"error":{"code":"DB_BROWSE_NOT_CONFIGURED","message":"The database browse is not configured on this install. Set DATABASE_READONLY_URL and restart the API."}}
```

**The rest of Hearth works**, checked by driving it rather than by asserting
it: `/money` rendered Finances with **S$12,345.67**, `/admin/mail` rendered
"Outbound mail", `/admin/flags` and `/admin/households` both answered 200.
This is the state production is actually in, and the product is unaffected by
it.

**State B — set, but pointing at a database that cannot be reached**
(`postgres://hearth_readonly:…@no-such-host.invalid:5432/hearth`).

**The API still boots** — this is the restore-day case and refusing here would
take the household product down over an operator panel. It logs, at ERROR:

```
DATABASE_READONLY_URL is set but the read-only database could not be opened;
the database browse will answer 503 and the rest of Hearth is unaffected
error="ping the read-only database: failed to connect to `user=hearth_readonly database=hearth`:
hostname resolving error: lookup no-such-host.invalid on 127.0.0.11:53: no such host"
```

The variable is named (pgx's own error would only have said `user=` and
`database=`) and **the DSN's password is not in the line** — pgx redacts it.

On screen, a different message pointing somewhere different:

> The read-only connection is not answering. Nothing is lost — the reader is:
> check that Postgres is up and that the `hearth_readonly` role still exists
> on it.

```
503 {"error":{"code":"DB_BROWSE_UNAVAILABLE","message":"The database browse cannot reach its read-only connection."}}
```

Neither response body contains the password. Two codes, two screens, two
different places to go — which is the whole reason the second code exists.

**The third boot outcome, confirmed rather than re-derived.** Task 7's
implementer already drove all three during its fix round; this walk
re-confirmed the security-critical one only, and records it as a confirmation
and not as its own finding. With `DATABASE_READONLY_URL` pointed at the
read-write `hearth` role, the API **refuses to boot**:

```
ERROR fatal error="ping the read-only database: DATABASE_READONLY_URL is misconfigured:
DATABASE_READONLY_URL connects as a role that may INSERT into users, so it is not a
read-only role. Point it at hearth_readonly (deploy/readonly-role.sql)"
[Process Exit with Code: 1]
```

Serving a "read-only" browse through a writable connection is worse than
serving nothing, and the process agrees.

The stack was then restored with a plain `docker compose up -d api`, and
`database browse enabled` is back in the log; `/admin/database` lists its 34
tables again with no alert.

**Result: PASS**, both states, plus the third boot outcome confirmed.

---

### 15. Four widths, both screens

**The highest-value criterion in the list**, because nothing had ever rendered
this in a real browser — task 9 could not, since nothing was serving the
frontend — and the nav now carries a **fourth** link where a third already
caused a 305px overflow once (the previous walk's `flex-wrap` fix). Both
screens were measured at each width, not just screenshotted: body overflow
from `documentElement.scrollWidth` vs `clientWidth`, the grid's own scroller
from its `scrollWidth` vs `clientWidth`, and the nav's wrap from the distinct
`top` values of its links.

The rows screen used `accounts` — 12 columns, a **1956 px** wide table, so the
overflow container is genuinely under load at every width.

| Width | Screen | Body overflow | Nav rows | Grid scrolls inside its own container | Panes |
|---|---|---|---|---|---|
| **305** | tables | **0 px** | **2** | — | — |
| **305** | rows | **0 px** | 2 | **yes** — 1956 px in a 273 px box | stacked |
| **360** | tables | **0 px** | 2 | — | — |
| **360** | rows | **0 px** | 2 | **yes** — 1956 px in a 328 px box | stacked |
| **768** | tables | **0 px** | **1** | — | — |
| **768** | rows | **0 px** | 1 | **yes** — 1956 px in a 486 px box | side by side (190 px pane) |
| **1440** | tables | **0 px** | 1 | — | — |
| **1440** | rows | **0 px** | 1 | **yes** — 1956 px in a 1158 px box | side by side |

**No horizontal scroll on the body at any width, on either screen.** The wide
table scrolls only inside its own `overflow-x-auto` container, exactly as the
comment on it claims. The two panes stack below `md` and sit side by side
above it.

**The nav wraps rather than overflowing.** At 305 px the five links occupy two
rows — `Flags` `Mail` `Households` at y=14, `Database` `Back to Hearth` at
y=37 — with the rightmost edge at **281 px** inside a 305 px header. The
fourth link did not reintroduce the overflow the third one caused; the
previous walk's `flex-wrap` absorbed it.

Screenshots, in
`docs/superpowers/plans/2026-09-04-hearth-database-browse-screenshots/`:

| File | What it shows |
|---|---|
| `db-tables-305.png` | Table list at 305 px — nav on two rows, `Database` bold on the second, blurb and table rows wrapping cleanly, nothing clipped |
| `db-rows-305.png` | `accounts` at 305 px — nav wrapped, columns pane stacked above the legend and grid, all 12 column names and types legible in one column |
| `db-tables-360.png` | Table list at 360 px — same two-row nav, more breathing room |
| `db-rows-360.png` | `accounts` at 360 px — still stacked, grid still scrolling in its own box |
| `db-tables-768.png` | Table list at 768 px — nav back to one row |
| `db-rows-768.png` | `accounts` at 768 px — panes side by side, 190 px columns pane, grid clipped at its container edge |
| `db-rows-1440.png` | `accounts` at 1440 px — the money reading (`DBS Joint Savings`, `1234567`, `SGD`), `«null»` in `owner_membership_id`, grid clipped mid-`opening_balance_curren…` and scrolling inside its box |
| `db-tables-1440.png` | Table list at 1440 px |
| `db-not-configured-1440.png` | Criterion 14 state A — the `DB_BROWSE_NOT_CONFIGURED` panel |
| `db-unavailable-1440.png` | Criterion 14 state B — the `DB_BROWSE_UNAVAILABLE` panel |

**Result: PASS** at all four widths on both screens.

---

## The defect

**Two comments explained `«null»` with an example this feature makes
impossible to observe.**

`domain/dbbrowse.go`'s comment on `NullCell` and `AdminDatabasePage.tsx`'s
comment on `Legend()` both said, in the same words:

> the difference is sometimes the bug being investigated (`users.password_hash`
> is NULL for a member who has only ever signed in with a magic link)

The first half is true — `password_hash` **is** NULL for Kayla and Ethan. The
implication is false: `ColumnIsRedacted` matches the `_hash` suffix, so that
column's value is replaced by a bind parameter inside the `SELECT` list and a
NULL in it renders `«redacted»`, never `«null»`. Confirmed on screen
(criterion 7). The one example offered for what `«null»` is *for* is the one
column where it cannot appear.

**Why it matters even though nothing misbehaves.** These are the two places a
junior engineer goes to find out what the marker means, and CLAUDE.md's rule
is that comments say *why*. A "why" that cannot be observed teaches someone to
look for `«null»` in the wrong place and to doubt the feature when they do not
find it. It is the same class as commit c993059, "docs: fix four claims in the
browse docs that do not survive checking" — and the uncomfortable detail is
that this one is *inside the code*, not in a document.

**The sharpest part: the test already knew.**
`TestRowsDistinguishesNullFromEmpty` asserts all three facts correctly —
`email` NULL → `«null»`, `display_name` `''` → empty, and, at line 142,
`password_hash` NULL → **`«redacted»`**. The test beside the comment had the
truth the whole time; only the prose drifted.

**The fix.** Both comments now name `users.email` (NULL for exactly those
magic-link members, and not redacted) and `goal_contributions` (a NULL date
beside an empty-string note on one row), and both explicitly warn off
`password_hash`, saying why:

> A redacted column never shows `«null»`, whatever it holds — the server
> substitutes the marker in the `SELECT` list rather than fetching the value,
> so NULL never survives to be seen.

**No new test, and that is deliberate.** The defect is prose; a test cannot
pin a comment. What the corrected comment now claims is already pinned by
`browse_repo_test.go:142`, so **that existing assertion was mutation-checked**
rather than a ceremonial new test being added. The redacted branch of
`BrowseRepo.Rows` was changed to make the old comment's implicit claim true:

```go
list = append(list, "CASE WHEN "+pgx.Identifier{c.Name}.Sanitize()+" IS NULL THEN "+
    placeholder(domain.NullCell)+" ELSE "+placeholder(domain.RedactedCell)+" END")
```

and the test went red:

```
--- FAIL: TestRowsDistinguishesNullFromEmpty (1.51s)
    browse_repo_test.go:143: a NULL in a redacted column rendered as "«null»", want "«redacted»"
FAIL
```

Reverted immediately, `git diff` clean on that file. The assertion really does
discriminate, so the corrected comment is a claim the suite will defend.

**The class sweep.** `grep` for `password_hash` alongside `null`/`magic link`
across `api`, `web`, `docs` and `.superpowers` found seven hits:

| Location | Verdict |
|---|---|
| `api/internal/domain/dbbrowse.go:13` | **Fixed** — the `NullCell` comment |
| `web/src/features/admin/AdminDatabasePage.tsx:283` | **Fixed** — the `Legend()` comment |
| `api/internal/adapter/postgres/browse_repo_test.go:360` | **Correct as written** — it describes what the fixture inserts, and the test it serves asserts the true behaviour. Left alone |
| `api/internal/adapter/postgres/convert.go:23`, `usecase/ports.go:53` | **Correct** — both are about the column being nullable in sqlc's `*string`, nothing to do with browse markers |
| `docs/superpowers/specs/2026-09-04-…-design.md:267`, `plans/2026-09-04-…-browse.md` (3 hits) | **Left alone, deliberately.** Specs and plans are records of what was decided at the time; commit c993059 set the precedent by fixing living docs (PROVISION, FEATURE_TRACKER, INFRASTRUCTURE, LEARNING) and not the historical spec. The claim is corrected where someone would act on it |

No second instance of the *behaviour* class exists: the only other marker,
`RedactedCell`, is explained by an example (`token_hash`) that is real and was
observed in criteria 5 and 6.

---

## Differences from the brief, found while walking it

**One, not two.** This section originally listed a second — a claim that
criterion 13's SQL was not runnable — which review found to be false and which
is retracted in full under criterion 13 above, with the four `psql` runs
recorded there: one disproves the claim outright, and the other three show why
it was wrong (the owner meeting the `NOT NULL` constraint the read-only role
never reaches, the `GRANT` refusing the same statement with only the read-only
guard bypassed, and an unknown column name failing at parse analysis before
either guard). The brief's criterion 13 was correct as written.

| The brief says | What is true | Consequence |
|---|---|---|
| Criterion 7: "`users.password_hash` is `NULL` for a magic-link-only member; seed one if the seeded household has none" | It is NULL, and it renders `«redacted»` — redaction is decided in the `SELECT` list and wins over NULL unconditionally | Following it literally produces a `«redacted»` where the criterion expects `«null»`, and looks like a product defect. Use `users.email` or `goal_contributions` |

---

## State changed on the dev box

- **One account exists that did not before**: `DBS Joint Savings`, S$12,345.67
  SGD, in the Andreas & Christine household, created through the product's own
  Add-account form for criteria 3 and 4 (see above). Left in place — it is
  ordinary product data, and `accounts` being empty is what made the walk
  awkward in the first place.
- **`admin_audit_log` grew from 160 to 230 rows.** Expected and correct: every
  read under `/admin` appends one, and this walk deliberately generated many —
  including 5 timing samples (criterion 2), 6 injection probes and 5
  malformed-range probes (criterion 11), and every viewport change in
  criterion 15 reloading both screens. Each is a genuine audited read.
- **`sessions` (28), `login_attempts` (24), `users` (6), `households` (2) are
  unchanged** by this walk — no sign-in was needed, and criterion 13's write
  attempts all failed as designed.
- **`hearth-api-1` was recreated four times** (unset, unreachable, writable,
  then restored). It is running normally on the committed configuration, with
  `database browse enabled` in its log. `hearth-web-1`, `hearth-postgres-1`
  and `hearth-mailpit-1` were untouched after the initial `up`.
- **`docker-compose.yml` is confirmed unchanged** — both criterion 14 states
  used override files in the session scratchpad, never an edit to the
  committed file. `git diff --exit-code docker-compose.yml` returned 0, run
  after the stack was restored.
- **Two comments changed for real**: `api/internal/domain/dbbrowse.go` and
  `web/src/features/admin/AdminDatabasePage.tsx`. No behaviour changed and no
  new test file was added — see "The defect" for why the mutation check was
  run against an existing assertion instead.
- **Mailpit's store was not touched**; no mail was generated by this walk.

---

## Final full run

```
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

**`make lint`**: architecture lint passed; `tsc --noEmit` clean; `eslint .`
clean; `go vet ./...` clean.

**`make test`**:

- Go suite (`go test ./... -count=1 -timeout=5m`, testcontainers against the
  colima Docker socket): **every package `ok`**, including
  `internal/adapter/postgres` (258.5 s) and `internal/adapter/http` (228.5 s)
  — the two carrying the browse's repository and route tests — plus
  `cmd/api` (6.5 s), which covers `openBrowse`'s four outcomes.
- Frontend (`npx vitest run`): **766 tests passed**.

Both green, on the tree carrying this walk's two comment fixes.
`docker-compose.yml` confirmed unchanged one more time immediately before
this section was written:

```
$ git diff --exit-code docker-compose.yml
$ echo $?
0
```
