# Hearth — feature tracker

Every feature in `design/Household Dashboard.dc.html`, and whether it exists
yet — plus a handful of rows the design does not draw, where a design feature
needed them to exist (see "Where things stand" below).

**Legend**

| | |
|---|---|
| ✅ | Built and verified |
| 🟡 | Partly built — the gap is named |
| ⬜ | Not started |
| 🚫 | Marked "· not built" by the design itself — out of scope by its own decision |

**Where things stand:** 43 of 91 features built or partly built. Money now has
two features built — Accounts (a household records what it owns and owes by
hand and sees a net worth built from it) and the Transactions ledger (logging,
editing and deleting expenses, income and transfers, with filters and the five
screen states the design and spec both call for). Nothing else in Money is
started yet, and nothing in Marriage, Family or Overview has been started.
Five of the rows below have no mockup of their own — the provisioning
transaction behind self-serve sign-up, the currency list endpoint, and
`adminctl prune` — because the design's own "Create household" screen (the
`authScreen` state, and the sign-in footer's "No household yet? Create one")
is what makes each of them necessary, and none has anywhere else to live in
this tracker. Accounts adds three more design-less rows, for a different
reason each: archive and restore, which the design never draws anywhere in
Money (see the Finances table below); and two ⬜ rows — custom account types
and a warning before a primary-currency change strands every account — for
work the accounts spec named and deliberately deferred rather than built.

| Area | Built | Partial | Not started | Design says no |
|---|---|---|---|---|
| Entry & authentication | 10 | 1 | 0 | 0 |
| Navigation shell | 6 | 0 | 1 | 0 |
| Household settings | 15 | 4 | 2 | 0 |
| Overview (home) | 0 | 0 | 8 | 0 |
| Money | 6 | 1 | 20 | 0 |
| Marriage | 0 | 0 | 13 | 0 |
| Family | 0 | 0 | 2 | 1 |
| Household extras | 0 | 0 | 0 | 1 |
| **Total** | **37** | **6** | **46** | **2** |

---

## What "built" means

A row only becomes ✅ when the feature works **and** the code behind it meets the
standards below. A feature that works but nobody can safely change is not
finished — it is a debt with a tick next to it.

**Clean architecture.** Dependencies point inward. `internal/domain` holds the
rules and imports the standard library only; `internal/usecase` orchestrates
them through ports it declares itself; adapters implement those ports. Any
database, HTTP or third-party type stops at the adapter boundary. `make
lint-arch` enforces this mechanically, including in test files.

**SOLID, as it actually applies here.**

- **Single responsibility** — one file, one job. `auth.go` does sign-in;
  `invite.go` does invites. When a file starts needing "and" to describe it,
  split it.
- **Open/closed** — extend by adding an adapter, not by editing a service.
  `FXRateProvider` exists precisely so a real rate source can arrive without
  touching the code that uses it. `BankSyncProvider` does not exist:
  Accounts, Money's first feature, ships manual entry only, which needs no
  port at all — a port with one implementation and no second caller is the
  wrong shape. One arrives when CSV import gives it a second implementation
  to abstract over.
- **Liskov** — an adapter must honour its port's whole contract, including
  errors. A missing row surfaces as `domain.ErrNotFound`, never `pgx.ErrNoRows`.
  A caller must not need to know which implementation it has.
- **Interface segregation** — narrow ports for what a caller needs. Nine small
  repositories, not one object with forty methods.
- **Dependency inversion** — services depend on interfaces they declare, and
  `main.go` decides the implementations. That is the whole reason the services
  are testable against in-memory doubles.

**Readable by a junior engineer on their first week.** This is a real
requirement, not a nicety:

- Names say what a thing is. Comments say **why** — never what the line already
  says.
- Small, focused files beat clever ones. If understanding a function needs three
  other files open, the seam is wrong.
- Exported things carry their contract in a doc comment. `usecase/ports.go` is
  the model: the `""` ⇄ SQL NULL convention and the transactional-accept warning
  live where the next implementer will actually read them.
- Every non-obvious decision is written down at the point someone would try to
  change it. Where a trade-off was accepted, the comment says so and why.
- Tests read as documentation. A test name states the behaviour; the body shows
  it.
- No cleverness in security-sensitive code. Obvious and boring wins.

**And the practical gate:** `make lint && make test` green, at least one new test
mutation-checked, and `docs/LEARNING.md` updated with anything the work taught.
The full checklist is at the end of `docs/LEARNING.md`.

---

## 1 · Entry and authentication

| Feature | State | Notes |
|---|---|---|
| Sign in with email and password | ✅ | No household details shown before authentication, per the design |
| Wrong-password state with attempts remaining | ✅ | "Two tries left", "One try left" — the design's copy verbatim |
| Household lockout after three failures | ✅ | Locks the household for 15 minutes, as the design's copy states |
| Magic link — request | ✅ | Always answers the same way whether or not the address exists |
| Magic link — sent panel | ✅ | Carries retry copy; the send is fire-and-forget so nothing else prompts it |
| Magic link — consume and sign in | ✅ | Works while the household is locked; that is the recovery path |
| Create household (self-serve sign-up) | ✅ | `POST /auth/sign-up` answers `202 {"status":"accepted"}` identically for a fresh, already-registered or rate-limited address — the same enumeration-safe contract as magic link. The design's single create card is split across two screens (email only, then the rest) so the person who clicks the mailed link supplies their own details, never a stranger's |
| Household provisioning | ✅ | One transaction creates the household, the owner, their membership, the three builtin spaces and notification preferences, with the sign-up token consumed first so two completions of one link can never both succeed |
| Invite acceptance | ✅ | Shows inviter, household and role; warns if you are already signed in as someone else |
| Sign out | ✅ | From the sidebar footer, returns to sign-in |
| "Forgot?" password recovery | 🟡 | Present, and triggers a magic link. There is no separate password-reset flow — recovery is `make reset-password` from the command line |

## 2 · Navigation shell

| Feature | State | Notes |
|---|---|---|
| Sidebar grouped into spaces | ✅ | Rendered from the server's own filtered, ordered list — not a hard-coded menu |
| Space visibility per member | ✅ | Money is capability-gated, Marriage is parents-only, Family is for everyone |
| Household footer with members and plan | ✅ | "Free plan" is static text, as specified |
| Modal primitive | ✅ | Native `<dialog>`; backdrop dismissal, Escape, focus trap. Slices 2–4 build on it |
| Placeholder pages for unbuilt areas | ✅ | Each names the slice that will ship it |
| `⌘K` command palette | ⬜ | Shown in the sidebar header; no behaviour behind it |
| "+ New space" | ✅ | See Household settings below |

## 3 · Household settings

| Feature | State | Notes |
|---|---|---|
| Members list with roles | ✅ | Owner and limited, with the design's own role labels |
| Member access switches | ✅ | Calendar, Chores, Money, Marriage. Marriage is never offered to a child |
| "Off for kids by default" on Money | ✅ | A child can be granted Money access; the design's toggle is real |
| Email addresses hidden from non-owners | ✅ | Owners see them; a limited member sees the list without addresses |
| Last-owner protection | ✅ | Removing or demoting the last owner is refused inline |
| Invite a family member (modal) | ✅ | Name, role, optional email, access switches |
| Remove a member | ⬜ | No control in the design either; the backend supports it |
| Spaces list with audiences | ✅ | |
| New space (modal) | 🟡 | Everyone and Parents only work. **Custom is shown disabled** — per-space membership is not built, and the design marks custom space pages "not built" too |
| Space templates — Kids, Home, Travel, Blank | 🟡 | Offered; they set a suggested name and visibility. They create no pages, because custom space pages are out of scope |
| Currency list (`GET /api/v1/currencies`) | ✅ | One server-served ISO 4217 list — only two-minor-unit codes are offered, since `Money.String()` hard-codes two decimal places. The frontend stopped keeping its own `CURRENCY_SYMBOLS` table; this backs both this panel and the sign-up form's currency select |
| Currency and region — primary currency | ✅ | |
| Currency and region — show second currency | ✅ | The toggle only — whether the second currency is shown. What it *is* is the row below |
| Currency and region — choose the second currency | 🟡 | No control exists to pick *what* the second currency is. A household that enables the toggle cannot choose what to compare against. Self-serve sign-up sets a household's second currency equal to its primary and leaves the toggle off, which makes this gap reachable by any stranger who signs up, not only Andreas & Christine |
| Currency and region — FX rate | 🟡 | The mode is stored and editable, but the rate itself is a fixed table. A live provider drops in behind the existing port |
| Notifications — bill due reminders | ✅ | |
| Notifications — overspend alerts | ✅ | |
| Notifications — monthly retro reminder | ✅ | |
| Notifications — weekly family digest | ✅ | |
| Retention pruning (`adminctl prune`) | ✅ | No UI — `cmd/adminctl prune --older-than=<days>` deletes consumed/expired `signups` and stale `login_attempts` past the cutoff. Refuses anything under a seven-day floor so it can never reach inside `domain.LockoutPolicy.Window` and clear a lockout that is still live |
| Connected accounts | ⬜ | Belongs with Money. Note that automatic bank sync is not available to an app like this — see Money below |

## 4 · Overview (home)

Nothing here is started. The page exists as a placeholder.

| Feature | State |
|---|---|
| Net worth card | ⬜ |
| July budget card — percentage used | ⬜ |
| Next bill card | ⬜ |
| Goals on track card | ⬜ |
| Next retro card with carried-over actions | ⬜ |
| Vision check-in strip | ⬜ |
| "This week" agenda | ⬜ |
| "+ Add" quick-create menu | ⬜ |

The "+ Add" menu offers Transaction, Account, Bill, Savings goal, Calendar event
and Marriage retro — so it depends on Money, Family and Marriage existing first.

## 5 · Money

Accounts and Transactions are built. Budget, Goals and Bills are still to
come; this is still the largest area.

**Finances**

| Feature | State |
|---|---|
| Net worth with 12-month trend | 🟡 |
| Assets and liabilities breakdown | ✅ |
| Accounts by owner, with SGD/IDR split | ✅ |
| Recent transactions strip | ⬜ |
| Link account — step 1, choose source | ⬜ |
| Link account — step 2, authorise | ⬜ |
| Link account — step 3, details and ownership | ⬜ |
| Manual account entry | ✅ |
| Archive and restore | ✅ |
| Custom account types | ⬜ |
| Warning in Settings before a primary-currency change strands every account | ⬜ |

**Net worth is missing only its 12-month trend.** The figure itself — assets
minus liabilities, converting each account into the household's primary
currency before summing — is built and shown live. The trend needs balance
snapshots: a second table, and a separate decision about when a snapshot gets
written (nightly? on every balance change? on read?). Deferred as its own
small spec, not folded into this one.

**Archive and restore is not drawn anywhere in the design.** There is no
remove control on the design's own account form or accounts list. An account
is never deleted — archiving stamps a timestamp, and a "Show archived" view
lists archived accounts with a restore action, so a mistake is recoverable.

**Custom account types and the primary-currency-change warning are deferred,
not missing by accident.** Five fixed types (`cash`, `investment`, `property`,
`loan`, `credit_card`) cover the design's own breakdown bars; a household
that wants something more specific, like "Gold savings" or "Arisan", names it
in the free-text nickname on a `cash` account instead. Custom types would also
need to be seeded at household creation, which reaches into the self-serve
sign-up provisioning transaction for a feature nobody has asked for yet — the
wrong trade today. The currency-change warning belongs to the Settings
screen: an owner changing the household's primary currency can strand every
account with no exchange rate to the new one, and the design has no copy or
control for warning them first. The state it would prevent is visible and
reversible without the warning (the screen already says when no account can
be converted, and changing the currency back restores the figure), which is
why this is deferred rather than treated as a defect.

**Link account — steps 1 and 2 of the design's chooser will not be built as
drawn.** SGFinDex access to real bank data is restricted to licensed
financial institutions (see the bank-sync note below), so the design's
"Connect a Singapore bank" card can never turn on. A chooser with one
permanently dead branch teaches nothing, so `+ Add account` opens step 3's
own form directly — built and counted as *Manual account entry* above, not as
a separate "step 3" row, since it is the same shipped modal under a different
name.

**Automatic bank sync is not buildable here.** SGFinDex access is restricted to
licensed financial institutions. The design's Singpass flow will be shown
unavailable; accounts arrive by manual entry or file import, behind a port that
a real aggregator could later fill.

**Transactions**

| Feature | State |
|---|---|
| Full ledger with filters | ✅ |
| Inline category editing | ⬜ |
| Add transaction (modal) | ✅ |
| Export CSV | ⬜ |

**Full ledger with filters and Add transaction share one modal, add and edit
alike** — the same `TransactionModal` Task 15 built, opened blank for a new
row (its own test: blank fields, POSTs on save) and opened populated for an
existing one clicked from the ledger. That click-to-edit path is also the
only caller `PATCH /transactions/{id}` has, and its own Delete control
(behind an in-page confirmation, never `window.confirm`) is the only caller
`DELETE /transactions/{id}` has. The ledger itself covers all five screen
states the spec's section 7.1 names: first run, filters matching nothing
(deliberately different copy from first run, so a household that filtered to
nothing doesn't think its ledger was wiped), transactions excluded from the
month's spend for want of an exchange rate, a row dated before its account's
opening balance (naming the account, since a transfer can predate one side
and not the other), and a disabled Add button when the household has no
accounts yet to attach a transaction to. "Load older transactions" appends a
second page held in local state, separate from the reactive first page —
editing or deleting a row that lives on an appended page patches or removes
it there directly, so a correction made on an older page is not left showing
its stale, pre-edit value.

**Inline category editing and Export CSV are the two pieces of the design's
Transactions screen still unbuilt.** Changing a transaction's category today
means opening the same edit modal used for everything else, not clicking the
category text directly in the ledger row. CSV export has no button anywhere
yet.

**Budget**

| Feature | State |
|---|---|
| Envelope per category with pace | ⬜ |
| Empty state with Family-of-four, 50/30/20 and import templates | ⬜ |
| Spending by person | ⬜ |
| Edit budget (modal) | ⬜ |
| Budget history (modal) | ⬜ |

**Goals**

| Feature | State |
|---|---|
| Savings goals with progress and funding source | ⬜ |
| Monthly contributions summary | ⬜ |
| New goal (modal) | ⬜ |

**Bills**

| Feature | State |
|---|---|
| Due-soon and paid-this-month timeline | ⬜ |
| Autopay status | ⬜ |
| Subscriptions summary | ⬜ |
| Add bill (modal) | ⬜ |

**Before building any of this**, the derived figures need defining. The design
shows `66% used`, `S$137/day left`, `on pace to save S$1,780`, `4 of 4 on
track`, net worth from assets minus liabilities, and unspent budget rolling into
a nominated goal at month end. None of those formulas are specified anywhere
yet.

## 6 · Marriage

Nothing started. Parents-only throughout.

| Feature | State |
|---|---|
| Retro history with mood | ⬜ |
| Mood chart over 12 months | ⬜ |
| Single retro view — went well, was hard, actions, notes | ⬜ |
| Start retro (modal) with mood, money check-in and actions | ⬜ |
| Vision — yearly theme | ⬜ |
| Vision — pillars with measures | ⬜ |
| Vision — longer-horizon milestones | ⬜ |
| Edit vision (modal) | ⬜ |
| Agreements by section | ⬜ |
| Agreements empty state with starter sets | ⬜ |
| Propose a change — add, edit, remove (modal) | ⬜ |
| New agreement section (modal) | ⬜ |
| Version history (modal) | ⬜ |

Agreements are the unusual one: every change goes through **propose → both
sign**, and history is preserved so a removed agreement can still be seen and
restored. That is append-only and versioned, not ordinary CRUD.

## 7 · Family

| Feature | State | Notes |
|---|---|---|
| Shared month calendar with per-person filters | ⬜ | Needs Bills, since bill dates appear on the grid |
| New event (modal) | ⬜ | |
| Kids view | 🚫 | The design marks it "· not built" |

## 8 · Household extras

| Feature | State | Notes |
|---|---|---|
| Custom space page — landing and "Add page" | 🚫 | The design marks it "· not built". Creating a space today adds the sidebar entry only |

---

## Suggested order

Dependencies, not preference:

1. **Money** — largest, the design's centre of gravity, and everything it needs exists
2. **Marriage** — independent of Money; Agreements is the interesting problem
3. **Family** — Calendar needs Bills for the bill dates on the month grid
4. **Overview** — last, because it only aggregates the three above. Building it
   earlier means stubbing everything it reads

Each area gets its own spec → plan → implementation cycle. See `docs/HANDOVER.md`
for what to settle before the first task of the next one.
