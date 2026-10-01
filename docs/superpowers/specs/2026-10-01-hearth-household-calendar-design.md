# One calendar per household — design

Date: 2026-10-01. Source: hearth-architect, from the QA run of the same day
(ISSUE-002 to ISSUE-006 and ISSUE-013 in the QA report). Product-owner decisions
are recorded at the end and are binding.

## Problem

Three clocks each decide "today": the server in UTC, the browser in its own
zone, and one frontend label that imitates UTC. For a UTC+8 household they
disagree eight hours a day, and about the month for the first eight hours of
every month.

What that looked like at 06:45–07:08 SGT on 1 Oct 2026 (22:45–23:08 UTC, 30 Sep):

- **ISSUE-002** — a holding purchase or price with the form's default date
  (browser today) is refused 422 "That date is in the future."
- **ISSUE-003** — the ledger opens on the server's month while the Add
  transaction form dates the row with the browser's day; the saved row is not
  in the list on screen.
- **ISSUE-004** — a bill paid today is missing from "paid this month", and its
  Undo with it; "Nothing due this month" above a bill due that day.
- **ISSUE-005** — a household created 1 Oct is offered "Start August retro".
- **ISSUE-006** — Goals "Actual this month S$0.00" beside a contribution dated
  today; the Portfolio report calls Q3 the current quarter.
- **ISSUE-013 (date half)** — transactions accept any date (a 2099 expense
  lowers today's balance) while holdings refuse today.

## What the code already says

- `households` has currencies only (`api/migrations/00002_identity.sql`); a time
  zone is a new column.
- A clock port exists: `usecase.Clock` (`api/internal/usecase/ports_platform.go`).
  Nearly every date-dependent service method already takes `today` as a
  parameter. `AccountService.validate`, `VisionService.CurrentYear` and
  Telegram's `LogSpend` still read a clock themselves. The missing piece is what
  the edge passes in.
- `docs/LEARNING.md` already names the fix as a household time zone, "not a
  patch here".
- Caller-local boundaries are rejected in code: `domain/period.go` (they "put
  the same trade in different quarters depending on who opened the page") and
  `adapter/http/retro_handlers.go` (the retro month comes never from the client).
- **A "day" must be a date, not a zoned instant.** The code normalises days two
  ways: read in the value's own zone (`period.go`, `refuseFutureDate` in
  `usecase/holding.go`, `domain.StartableMonth`) and convert to UTC first
  (`domain.NextDue`, `billStartOfDay` in `usecase/bill.go`). They agree only on
  "household calendar day stamped midnight UTC", the shape `usecase/nudge.go`
  already builds. Passing `now.In(loc)` would break Bills for eight hours a day.
- Three sites skip the household's day entirely:
  `adapter/http/transaction_handlers.go` (raw `time.Now().UTC()` for the default
  month), `usecase/telegram_command.go` (`/spend` dated by UTC day),
  `usecase/vision.go` (year off the UTC clock).
- `BillService.SetArchived(at)` uses one value for two jobs: the `archived_at`
  stamp and the view's "today".
- The opening-balance one-day slack in `usecase/account.go` exists only because
  no zone is stored.
- Retro intent (`docs/superpowers/specs/2026-08-16-hearth-retros-design.md`):
  the startable month is the earlier of {previous, current} with no row.
- The frontend duplicates `today()` on purpose, eight times; `BillsPage.tsx`
  formats one label with `timeZone: "UTC"` to match the UTC server.
- `/auth/me` already carries the household; `Scope` is built in two places
  (`middleware_session.go`, `middleware_token.go`).
- `hearthctl` requires `--date`; unaffected. No SQL uses `CURRENT_DATE`.
- Test trap (`adapter/http/api_test.go`): session expiry is checked by Postgres
  `now()`, so the test clock must not be pinned to the past before sign-in.

## Options considered

| | A. Household time zone | B. Client sends its date/month | C. Everyone uses UTC |
|---|---|---|---|
| What | One IANA zone per household; server and frontend both compute "today" in it | A `today`/`month` parameter on every derived read; one-day slack on writes | Frontend computes UTC too |
| Telegram and digest | Fixed by the same function | Not fixed (no client) | Consistent but wrong |
| Members in two zones | One shared calendar | Two people see different months over shared money | One calendar, wrong for everyone |
| Undo | Drop one column | Revert contracts | Trivial |

**Chosen: A.** B is rejected on the rule in `period.go`. C makes the screen
agree with the server by making both wrong to the person.

## The design

1. `households.timezone` stores an IANA name.
2. One pure function, `domain.TodayIn(now time.Time, zone string) (time.Time, error)`,
   returns the household's calendar day stamped midnight UTC and fails closed on
   an unknown zone.
3. It has two callers: the HTTP auth middleware (sets `Scope.Today` once per
   request) and `TelegramCommandService`.
4. The frontend computes the same day from `me.household.timezone` through one
   helper, never from the browser's zone.

Members in different zones: the household's zone wins. A partner in London at
23:30 sees tomorrow's date as the default. Accepted cost of one shared calendar.

Retro on the 1st: the month just ended (September on 1 Oct), as the spec says.

Future-date rule: **a recorded fact may not be dated after household-today; a
plan may.** Facts: purchase, sale, price, income, opening balance, contribution,
payment, transaction. Plans: bill next-due, goal target month, budget month.

## What it touches

### Backend

| Layer | File / symbol | Change |
|---|---|---|
| Migration | `api/migrations/00022_household_timezone.sql` | `timezone text NOT NULL DEFAULT 'UTC'`; existing rows set to `Asia/Singapore` (owner decision 1) |
| Domain | new `api/internal/domain/household_day.go` | `ParseTimezone` (refuses `""` and `"Local"`, which Go silently maps to UTC and the server's zone), `TodayIn`, `IsAfterDay` |
| Domain | `identity.go` `Household` | `Timezone string` |
| Usecase | `HouseholdService.Update`, `signup.go` | validate via `ParseTimezone` |
| Usecase | `telegram_command.go` `TelegramCommandDeps` | add `Households`; `/spend` uses `TodayIn` |
| Usecase | `bill.go` `SetArchived` | split into `(at, today)` |
| Usecase | `account.go` `Create`/`Update`/`validate` | take `today`; drop the slack; use `IsAfterDay` |
| Usecase | `holding.go` `refuseFutureDate` | delegate to `IsAfterDay` |
| Usecase | `vision.go` `CurrentYear` | take `today`; drop the Clock |
| Postgres | `queries/identity.sql` and the repo mapping | carry the column; regenerate sqlc |
| HTTP | `Scope` and both auth middlewares | `Today`, set by one shared helper; a zone that fails to load is a logged 500 |
| HTTP | `householdDTO`, `updateHouseholdRequest`, sign-up request | `timezone` |
| `cmd/api/main.go` | `import _ "time/tzdata"` | without it an image lacking zone data fails every authenticated request |

Handler call sites that change from `deps.Clock.Now()` to `scope.Today`
(line numbers as read at `4b43b60`; find by symbol, they will have moved):
`account_handlers.go`, `budget_handlers.go` (three), `goal_handlers.go` (four,
plus the view half of archive), `bill_handlers.go` (four, plus the view half of
archive), `holding_handlers.go` (five), `retro_handlers.go` (two),
`transaction_handlers.go` (default month), `vision_handlers.go`.

These stay on the Clock because they are instants: every `archived_at` stamp,
retro `Finish` and `SetActionDone`, sessions, tokens, lockouts, invites, admin
grants.

### Frontend

- New `web/src/lib/householdDate.ts`: pure `todayIn(zone)` and `monthIn(zone)`
  built on `Intl.DateTimeFormat` with `timeZone`, plus a hook reading `useMe()`.
- `householdSchema` gains `timezone: z.string().default("UTC")`.
- Replace the eight `today()` copies: `HoldingLotsPanel`, `HoldingIncomePanel`,
  `BillModal`, `TransactionModal`, `GoalContributionsPanel`, `AccountModal`,
  `MarkPaidModal`, `PortfolioReportPage`.
- Replace the other browser-clock reads: `month.ts` `currentMonth`,
  `GoalModal.currentMonthValue`, `SetupChecklist.monthName`,
  `TransactionsPage.formatDateHeading`, `visionQueryKeys.ts`.
- `BillsPage.tsx`'s `timeZone: "UTC"` label must change in the same commit as
  the Bills server step, or it recreates ISSUE-006.
- `SignUpCompleteScreen.tsx` sends the browser's zone beside the currency.
- Settings gets a zone select beside `CurrencyPanel`.

This reverses the documented "duplicate on purpose" decision: the helper now
depends on household data, so a private copy is a private bug.

### Comments and docs that become false

`period.go` (caller-local comment), `account.go` (slack comment),
`transaction_repo.go` (`startOfMonth`), `networth_trend.go`, `domain/errors.go`,
`SYSTEM_DESIGN.md`, `LEARNING.md` (the "not a patch here" entry). Also update
`FEATURE_TRACKER.md`, `GUIDE.md`, `CLI.md`, and add ADR 12 (text below).

## Build order

1. Domain file and its tests. No behaviour change.
2. Migration, field, repo, DTO, `PATCH /household`, tzdata import. Nothing reads
   the zone yet.
3. `Scope.Today` in both middlewares.
4. One slice per commit, each with its pinning test: holdings (002);
   transactions default month (003); bills, including the `SetArchived` split
   and the `BillsPage` label (004); retros (005); goals, portfolio report,
   budget, accounts summary (006); opening-balance slack; vision year;
   Telegram `/spend`.
5. Frontend helper, then one consumer group per commit.
6. Sign-up sends the zone; Settings select.
7. The comment and doc sweep, plus the ADR.
8. The owner-decided rules (below): the unified fact rule, the retro floor, the
   Overview card.

## Tests that pin each defect

**Faking the clock.** Usecase tests use `fixedClock`
(`usecase/testdouble_test.go`) with exact instants such as
`2026-09-30T23:00Z`. HTTP tests use `movableClock`, anchored forward to the next
23:00 UTC at or after real now; assert dates derived from the anchor, not
literals. The transactions default month needs a month boundary: sign in at real
time, then set the clock to a month-end 23:00Z; if that cannot work with the
session-expiry trap, pin that case at usecase level instead. Frontend tests use
`vi.useFakeTimers({ toFake: ["Date"] })` with `process.env.TZ` set to a zone
different from the household's — that difference is the point of the test.

**Per defect.**
- Domain: `TodayIn(2026-09-30T23:00Z, "Asia/Singapore")` is `2026-10-01T00:00Z`;
  a western zone gives the day before; `""`, `"Local"` and nonsense are refused.
- Domain shape guard: `billStartOfDay(TodayIn(...))` equals its input.
- 002: household on Singapore, clock at 23:00Z on day D: an event dated D+1 is
  201, D+2 is 422. Control: household on UTC, D+1 is 422 (this is the mutation
  check).
- 003: `GET /transactions` with no month returns the household's month.
- 004: a bill paid on local today appears in `paidThisMonth`; a bill due that
  day counts in `dueThisMonth`.
- 005: `Start` at that instant creates September, not August.
- 006: goals `actualThisMonth` includes a contribution dated local today; the
  report's current period is Q4.
- Frontend: each form default, `NextRetroCard`, `SetupChecklist` and the Bills
  label show the household's day with a Los Angeles browser.
- Browser walk: set the zone to `Pacific/Kiritimati` or `Pacific/Pago_Pago`; one
  of them is on a different date from UTC at any hour, so the walk does not
  depend on when it runs.

## Risks

- Test churn: if most component tests break on the `useMe` dependency, pass the
  zone as a prop from the page instead.
- The digest still uses `NUDGES_TIMEZONE` for its day; a household in another
  zone linking Telegram needs one more column on `ListNudgeRecipients`. Not
  built now.
- If the per-request household read shows up in a measurement, fold the zone
  into the membership query.

## Deliberately not built

A `TodayProvider` port or a `Day` type; per-member zones; per-household digest
send time; hiding future-dated rows from balances until their date; a
server-sent `today` field (it goes stale at midnight in a cached query); any
change to hearthctl's required `--date`; a confirmation for a row saved into a
month other than the one shown (a UX item, not a clock defect).

## Product-owner decisions (2026-10-01)

1. **One time zone per household; existing households are set to
   `Asia/Singapore`** in the migration. The column default for new rows is
   `'UTC'`; sign-up sends the browser's zone.
2. **Future-dated facts are refused**: transactions, bill `paidOn` and goal
   contributions join holdings and opening balances under the one rule. This
   reverses the note in `usecase/ports_money.go` that Balance has no upper date
   bound on purpose. Existing rows stay; a PATCH is checked only when it changes
   the date.
3. **Retro floor at the creation month**: a household is never offered a retro
   for a month before the one it was created in. This changes the retro spec.
4. **Overview on the 1st with last month's draft still open shows the open
   draft**, not a prompt for the new month. This changes the Overview card spec.

## ADR 12 (text to add as `docs/adr/0012-one-calendar-per-household.md`)

**Status:** accepted, 2026-10-01.

**Context.** The server computed "today" in UTC and the browser in its own zone.
For a UTC+8 household they disagreed eight hours a day, and about the month for
the first eight hours of every month (QA 2026-10-01, ISSUE-002 to 006; LEARNING
pattern 1).

**Decision.**
1. A household has one IANA time zone, `households.timezone`, captured at
   sign-up and editable by an owner.
2. "Today" is the household's calendar day, computed by `domain.TodayIn` and
   carried inward as midnight UTC. Code never passes a zoned instant where a day
   is meant.
3. Each inbound edge computes it once: the HTTP middleware into `Scope.Today`,
   and the Telegram command service.
4. The frontend derives dates from the household's zone, never the browser's.
5. Instants (sessions, stamps, expiries) stay UTC on `usecase.Clock`.
6. A recorded fact may not be dated after household-today; a plan may.

**Consequences.** A travelling member sees the household's date. Changing the
zone rewrites no stored data. The digest's `NUDGES_TIMEZONE` remains an
install-level send time, and is a second source of "today" until a household
outside that zone needs otherwise. The rule in `period.go` that boundaries do
not depend on who opened the page is kept.

**Rejected.** Client-supplied dates: no client on Telegram or the digest, and
two members would disagree. UTC everywhere: consistent and wrong.
