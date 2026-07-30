# Finance day-one fixes — definition-of-done walk

Walked 2026-07-30 against `fix/finance-day-one`, in a real browser (Chrome,
driven via the Chrome DevTools MCP tools) on a wiped database: `make down`,
`docker volume rm hearth_hearth-pgdata`, `make up && make seed`.

**Result: 5 of 5 criteria pass on the first walk.** No product defect
surfaced. One tooling snag did (criterion 3, below) — caught, corrected, and
re-verified before recording pass, exactly as the "record a failure plainly"
standard requires even when the failure was mine rather than the product's.

Screenshots referenced below are in
`docs/superpowers/plans/2026-07-30-hearth-finance-fixes-screenshots/`.

---

## Before the criteria: environment

Both Docker engines were checked before starting, per the two-engine trap in
`docs/LEARNING.md`. This time there was no trap to fall into: colima's socket
did not exist at all (`dial unix
/Volumes/Oink_Machine/.colima/default/docker.sock: connect: no such file or
directory` — colima was not running on this machine for this session), and
`docker context show` confirmed the active context was `desktop-linux`
(Docker Desktop), which was the only engine holding the running `hearth-*`
containers. One engine, one stack — nothing to reconcile.

Worth flagging for whoever runs this walk next: the task brief's Go-test
environment note (`export
DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock`) is
stale for this machine's current state. Exporting it before `make test` would
have pointed testcontainers at a socket that does not exist. `make test` was
run instead against the default context (Docker Desktop), with only
`TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock` set — see "The
gate" below.

`docker volume ls | grep pgdata` confirmed the volume name the brief names,
`hearth_hearth-pgdata`, was the one to drop — not a worktree-prefixed
variant. Reset ran clean: `make down`, `docker volume rm
hearth_hearth-pgdata`, `make up && make seed`, producing the seeded household
"Andreas & Christine" (Andreas / `hearth-dev-password`) with no errors.

---

## The criteria (verbatim from the task brief)

1. Sign up a **fresh** household (not the seed). Create an account dated
   today, log an expense — Finances balance and net worth drop by exactly the
   amount, on screen, without a manual reload.
2. Log income — both rise by exactly the amount.
3. Log an expense dated before the account's opening date — excluded, and the
   ledger row is marked with the account's name.
4. Sidebar shows the MONEY group with Finances and Transactions; both
   navigate; the accent follows the route (exact-match on Finances —
   `/money/transactions` must not accent both).
5. Add-account modal shows the generic limited-members line on this
   household (it has none); in the seeded household it names the seeded
   children. No design-household name appears anywhere clicked through
   (Money, Settings).

---

## Results

| # | Criterion | Result |
|---|---|---|
| 1 | Fresh household, same-day account, expense drops balance & net worth exactly, no manual reload | **PASS** |
| 2 | Income raises both by exactly the amount | **PASS** |
| 3 | Pre-opening expense excluded from balance, ledger row marked with the account's name | **PASS** |
| 4 | Sidebar MONEY group, both links navigate, accent exact-matches the route | **PASS** |
| 5 | Fresh household gets the generic limited-members line; seeded household names Ethan & Kayla; no design-household name leaks anywhere clicked through | **PASS** |

### What each one actually showed

**Criterion 1 — a fresh household, a same-day account, and a same-tab proof
that "without a manual reload" really holds.** Signed up through the real
self-serve flow: `/sign-up` → email `walk-fresh@hearth.family` → magic link
fetched from Mailpit's API (`GET /api/v1/messages`, then the message body) →
household setup form → "The Fresh Household", SGD, owner "Walker Fresh". An
account, "Everyday Checking" (Cash & savings, Shared), was created with a
starting balance of S$1,000.00 and its `Starting balance as of` left at its
default — **today, 2026-07-30** — the exact boundary Task 2's same-day fix
exists for. Two expenses dated today were then logged from the Transactions
page ("Groceries at the corner store" −S$75.50, "Coffee run" −S$40.00), and
the same browser tab was navigated back to Finances via the router's own
"‹ Finances" link — a client-side route change, never a hard refresh. Net
worth and the account balance both read **S$884.50**
(`1,000.00 − 75.50 − 40.00 = 884.50`, exact), reflected immediately on
landing, before any `F5`
(`criterion-1-before-expense-networth-1000.png`,
`criterion-1-after-expense-networth-884-50.png`). This also proves the
same-day rule itself: an expense dated the same day as the account's own
opening balance still moved that balance, which is exactly what Task 2's fix
was for.

One thing worth recording rather than treating as a defect: an earlier
attempt tried to prove "already-open, no reload" literally, by opening
Finances in one browser tab and adding the expense in a second tab, then
switching back to the first without navigating it. That tab never updated,
even after forcing a synthetic `focus`/`visibilitychange` event. This is
correct behaviour, not a bug — each tab gets its own in-memory
`QueryClient` (`web/src/main.tsx`), so one tab's mutation has nothing to
invalidate in another tab's cache; there is no cross-tab sync in this app
and the brief never asked for one. The criterion's "without a manual reload"
is about avoiding `F5` within a single session, which the single-tab
SPA-navigation walk above proves directly.

**Criterion 2 — income, same session, same exactness.** From the same tab
and account, a S$300.00 "Freelance payment" income transaction (dated
today) was logged, and navigating back to Finances (again client-side, no
reload) showed net worth and the account balance both at **S$1,184.50**
(`884.50 + 300.00`, exact) (`criterion-2-income-networth-1184-50.png`).
"Spent this month" stayed at S$115.50 — income correctly did not move it.

**Criterion 3 — excluded from the balance, marked with the account's name,
still counted in month spend.** A third expense, "Old subscription charge"
for S$50.00, was logged dated **2026-07-01** — before the account's own
2026-07-30 opening date. The first attempt to set that date silently failed:
the composite date control's `fill` call reported success and the
accessibility snapshot echoed back `2026-07-01`, but the transaction actually
saved with today's date — caught immediately by reopening the row's edit
modal and seeing `2026-07-30` still there, not a product defect, a quirk of
driving a native composite `<input type="date">` through this tool. Fixed by
setting the underlying input's value with the DOM's native property setter
and dispatching real `input`/`change` events, then re-saving; the row then
correctly sorted under a `JUL 1` date header and read: **"Before Everyday
Checking's opening balance — it doesn't change that balance."** — naming the
account, not a generic message
(`criterion-3-pre-opening-expense-marked.png`). Net worth and the account
balance stayed at S$1,184.50, unmoved by the pre-opening expense
(`criterion-3-balance-unchanged.png`), while "Spent this month" rose to
S$165.50 (`115.50 + 50.00`) — excluded from the balance, but still counted in
the month's spend, exactly as the criterion (and the same rule Task 2 fixed
for balances) requires.

**Criterion 4 — exact-match, both directions, via `getComputedStyle`.**
Checked with `getComputedStyle(el).color` against the sidebar's own
`[data-testid="sidebar-space"]` links rather than by eye. At `/money`:
Finances read `rgb(26, 107, 82)` (`#1a6b52`, the accent) and Transactions
read `rgb(28, 27, 24)` (`#1c1b18`, ink). At `/money/transactions`, the two
flipped exactly: Transactions became `rgb(26, 107, 82)` and Finances fell
back to `rgb(28, 27, 24)` — proving the exact-match the criterion names:
`/money/transactions` does not also accent Finances
(`criterion-4-sidebar-transactions-accent.png`). Both links navigated
correctly in every click of this walk. One observation outside the
criterion's own scope: `Overview` in `Sidebar.tsx` is unconditionally
`text-accent` regardless of route (a static design choice per that file's
own comment), so it reads accented on every page including `/money` and
`/money/transactions` — pre-existing, not part of criterion 4's Finances/
Transactions pair, and not a defect.

**Criterion 5 — both halves, plus the stranger's-eye sweep for a leaked
name.** On the fresh household ("The Fresh Household", zero limited
members), opening the add-account modal before any account existed showed
the generic line: **"Limited members can see this account exists, not the
balance"** (`criterion-5-fresh-household-generic-line.png`). Switching to
the seeded household (`andreas@hearth.family` / `hearth-dev-password`,
whose Settings page confirmed two limited members, Ethan and Kayla) and
opening the same modal showed **"Ethan & Kayla can see this account exists,
not the balance"** — real names from real membership data, not the design's
hardcoded "Kayla & Ethan" string
(`criterion-5-seeded-household-names-real-members.png`). Per the task's own
context, this is correct behaviour for the seeded household, not a
regression of the fresh-household fix. Separately, on the fresh household,
`document.body.innerText.match(/Kayla|Ethan|Christine|Andreas/g)` was run on
Overview, Finances (both before and after the criterion-1/2/3 transactions),
Transactions, and Settings — `null` on every page, confirming no
design-household name leaked anywhere clicked through.

---

## The gate

`make lint && make test` was run at the end of this walk. Per the
environment note above, `make test`'s Go suite ran against Docker Desktop
(the only engine actually running), with `TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`
set and the brief's colima `DOCKER_HOST` override deliberately **not** set
— exporting it would have pointed testcontainers at a socket this machine
did not have this session. Both `make lint` and `make test` finished green
on the tree being integrated. Full output is in the task report.

Commit: recorded alongside this file, in the task report.
