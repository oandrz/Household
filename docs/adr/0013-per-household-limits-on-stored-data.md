# 13. Per-household limits on stored data

**Status:** Accepted — 2026-10-16.

## Context

Hearth is offered to self-serve households, all on one shared server with one
disk ([ADR 2](0002-first-production-host.md)). Until now nothing bounded how
much one household could store: how many transactions, accounts or notes, how
long a name or a description could be, or which months a budget could be
saved for. That left two things depending only on the size of the disk:

- how much space one household can take from everyone else's;
- how large some reads get. A month's summary and the budget screen load every
  transaction in the month, and their cost grows with that count.

Real households are nowhere near any number below: on the day this was
decided the busiest production household held 8 transactions and 13
categories, and its longest description was 14 characters.

## Decision

Each household has a ceiling on what it can store, and text has a length
limit. The numbers live in `api/internal/domain/limits.go` and
`api/internal/domain/text_limits.go`, each with its reason beside it. Each cap
is roughly four to five times what a very heavy real family would reach.

| Group | What | Cap per household | Why this number |
|---|---|---|---|
| Transactions in one month | dated in one calendar month | 3,000 | A very busy family logs about 20 a day, 600 a month. Bounds every read that loads a whole month. |
| Transactions in total | all transactions | 100,000 | About 14 years of that very busy family, 40 years of a normal one. Needed as well as the monthly cap because a transaction's date has no lower bound. |
| Named things | accounts, categories, goals, bills, holdings, spaces, agreement sections, each separately | 200 each, **archived included** | These are archived rather than deleted, so archive-and-recreate must not get round the cap. |
| Records | goal contributions, bill payments, holding buys and sells, valuations, income, retro actions, agreement proposals, each separately | 20,000 each | A contribution every day for fifty years. Agreement proposals are never deleted, so this is their only bound. |
| People | members; pending invites | 30; 20 | Far above any household. Expired invites are pruned daily. |
| API tokens | neither revoked nor expired | 20 at once | Spent tokens are pruned after 30 days, so rotating them does not pile up rows. |
| Budgets | which months can be budgeted | January 2000 to 24 months ahead | Each month is its own row; without a window nothing bounds how many there are. |

| Text | Limit (characters) |
|---|---|
| Names: household, family, member, invitee, space, account, category, goal, bill, holding | 80 |
| Transaction description | 200 |
| Notes: goal contribution, holding buy/sell, valuation, income; retro action | 500 |
| Holding unit | 20 |

Characters are counted as a person sees them (`utf8.RuneCountInString`,
Postgres `char_length`), after trimming surrounding spaces.

**How it is enforced.**

- Caps are checked in the usecase layer, inside each service's create method,
  through one port, `usecase.HouseholdUsage`. Every channel passes through
  them: the browser, `hearthctl`, API tokens and the Telegram bot. Who is
  asking is still decided only at each channel's edge
  ([ADR 8](0008-authorisation-at-each-channels-inbound-edge.md)); a cap never
  answers before `401` or `403`.
- A bill payment writes a ledger row too, so it meets the transaction caps as
  well as its own.
- Text limits sit beside each field's other rules, and each capped column also
  carries a database `CHECK` at the same limit, as a second line.
- A reached cap answers `409 LIMIT_REACHED`, naming the thing and the number
  (and the month, for the monthly cap). Text over its limit answers `422
  TEXT_TOO_LONG`. A budget month outside the window answers `422
  BUDGET_MONTH_OUT_OF_RANGE`. The bot explains both rather than saying "try
  again".
- A keyed retry whose first attempt already landed is answered "already
  saved" even at the cap, because the bot and `hearthctl` resend after a lost
  reply. Replacing a holding's valuation for a day that already has one adds
  no row, so it is allowed at the cap.

## Who can reach a limit, and who is affected

Any member of the household, the household's own API token, or its Telegram
bot. Only that household is affected: it cannot add more of that thing until
it removes some. No other household notices.

Named things can only be archived, so a household that reached 200 of one
would stay at 200. The owner accepted this at that number.

## Accepted trade-offs

- **Count, then write, is not atomic.** Two saves in the same instant can
  both see one place left and both land, leaving a household a few rows over
  a cap. Accepted rather than adding locks: the overshoot is bounded by the
  requests in flight at that moment, and the cap exists to bound disk use,
  not to be an exact count.
- **A month's reads still load the month's rows** (`TransactionRepository.
  MonthTotals`), now bounded by the monthly cap. A narrower second query was
  rejected: it would save memory the cap already bounds and add a second way
  of reading a month that could drift from the first.

## What this does not cover

Caps bound one household. Many households, each within its own caps, are
bounded only by the sign-up limits. The disk alarm (`deploy/disk-check.sh`,
`deploy/PROVISION.md` §12) is the watch for that.

## Revisit when

- bank or CSV import arrives, which can bring years of history at once and
  may need a higher total transaction cap;
- any real household passes half of any cap;
- the monthly cap is to be raised a lot, at which point reading a month as
  narrow rows becomes worth its second code path.
