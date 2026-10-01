# 12. One calendar per household

**Status:** Accepted — 2026-10-01.

## Context

The server computed "today" in UTC and the browser in its own zone. For a
UTC+8 household they disagreed eight hours a day, and about the month for the
first eight hours of every month (QA 2026-10-01, ISSUE-002 to 006;
`docs/LEARNING.md` pattern 1).

What that looked like at 07:00 in Singapore on 1 October, while the server was
still on 30 September:

- a holding purchase with the form's default date was refused, "That date is
  in the future";
- a transaction saved from the ledger was not in the list on screen;
- a bill paid that morning was missing from "paid this month", and its Undo
  with it;
- a new household was offered "Start August retro";
- Goals read "Actual this month S$0.00" beside a contribution dated today.

Each one had been patched before where it surfaced. `docs/LEARNING.md` had
already named the real fix as a stored household time zone.

## Decision

1. A household has one IANA time zone, `households.timezone`, captured at
   sign-up and editable by an owner.
2. "Today" is the household's calendar day, computed by `domain.TodayIn` and
   carried inward as midnight UTC. Code never passes a zoned instant where a
   day is meant.
3. Each inbound edge computes it once: the HTTP middleware into `Scope.Today`,
   and the Telegram command service.
4. The frontend derives dates from the household's zone, never the browser's.
5. Instants (sessions, stamps, expiries) stay UTC on `usecase.Clock`.
6. A recorded fact may not be dated after household-today; a plan may. Facts
   are a purchase, sale, price, income row, opening balance, contribution,
   payment and transaction. Plans are a bill's next due date, a goal's target
   month and a budget month.

Why a day is carried as midnight UTC (point 2): Hearth's date rules read a
day in two ways. Periods and retros read a value in its own location; Bills
convert it to UTC first. A zoned instant gives those two families different
days. A date stamped midnight UTC is the one shape both read the same way.
`TestTodayInMeansTheSameDayToBothFamiliesOfDateRule` and
`TestAZonedInstantIsNotAHouseholdDay` (`api/internal/domain/household_day_test.go`)
pin both halves.

## Consequences

- A travelling member sees the household's date. A partner in London at 23:30
  sees tomorrow's date as the default for a household in Singapore. That is
  the accepted cost of one shared calendar.
- Changing the zone rewrites no stored data. It changes which day counts as
  today from the next request on.
- A household whose stored zone cannot be loaded gets a logged 500 on every
  authenticated request, never a date worked out some other way. The API
  refuses such a zone on the way in, so this only happens to a row edited
  around it, and the fix is then a direct database edit: the Settings control
  is behind the same middleware.
- Every authenticated request reads the household row once more than it did.
  If that ever shows in a measurement, carry the zone on the membership
  query.
- The daily digest's `NUDGES_TIMEZONE` remains an install-level send time, and
  is a second source of "today" until a household outside that zone needs
  otherwise.
- The rule in `api/internal/domain/period.go` that a boundary does not depend
  on who opened the page is kept. The household's zone decides it, not the
  caller's.

Point 6 is enforced today for holdings (purchase, sale, price, income) and for
an account's opening balance. Transactions, bill payments and goal
contributions still accept any date; bringing them under the rule is listed in
`docs/FEATURE_TRACKER.md`. When it is, existing rows stay, and an edit is
checked only when it changes the date, which is already how an account's
opening balance behaves.

## Rejected

- **Client-supplied dates** (a `today` or `month` parameter on every derived
  read): there is no client on Telegram or in the digest, and two members in
  two zones would see different months over the same shared money.
- **UTC everywhere**, the frontend included: consistent, and wrong for
  everyone who is not in UTC.
- **A server-sent `today` field**: it goes stale at midnight inside a cached
  query.
- **A `TodayProvider` port or a `Day` type, per-member zones, and a
  per-household digest time**: not needed to fix the defect, so not built.
