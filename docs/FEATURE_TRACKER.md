# Hearth — feature tracker

Every feature in `design/Household Dashboard.dc.html`, and whether it exists yet.

**Legend**

| | |
|---|---|
| ✅ | Built and verified |
| 🟡 | Partly built — the gap is named |
| ⬜ | Not started |
| 🚫 | Marked "· not built" by the design itself — out of scope by its own decision |

**Where things stand:** 31 of 83 features built or partly built. Everything
complete belongs to entry, identity and household settings; nothing in Money,
Marriage, Family or Overview has been started.

| Area | Built | Partial | Not started | Design says no |
|---|---|---|---|---|
| Entry & authentication | 8 | 1 | 0 | 0 |
| Navigation shell | 6 | 0 | 1 | 0 |
| Household settings | 13 | 3 | 2 | 0 |
| Overview (home) | 0 | 0 | 8 | 0 |
| Money | 0 | 0 | 24 | 0 |
| Marriage | 0 | 0 | 13 | 0 |
| Family | 0 | 0 | 2 | 1 |
| Household extras | 0 | 0 | 0 | 1 |
| **Total** | **27** | **4** | **50** | **2** |

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
| Currency and region — primary currency | ✅ | |
| Currency and region — show second currency | ✅ | |
| Currency and region — FX rate | 🟡 | The mode is stored and editable, but the rate itself is a fixed table. A live provider drops in behind the existing port |
| Notifications — bill due reminders | ✅ | |
| Notifications — overspend alerts | ✅ | |
| Notifications — monthly retro reminder | ✅ | |
| Notifications — weekly family digest | ✅ | |
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

Nothing started. This is the largest area and the recommended next slice.

**Finances**

| Feature | State |
|---|---|
| Net worth with 12-month trend | ⬜ |
| Assets and liabilities breakdown | ⬜ |
| Accounts by owner, with SGD/IDR split | ⬜ |
| Recent transactions strip | ⬜ |
| Link account — step 1, choose source | ⬜ |
| Link account — step 2, authorise | ⬜ |
| Link account — step 3, details and ownership | ⬜ |
| Manual account entry | ⬜ |

**Automatic bank sync is not buildable here.** SGFinDex access is restricted to
licensed financial institutions. The design's Singpass flow will be shown
unavailable; accounts arrive by manual entry or file import, behind a port that
a real aggregator could later fill.

**Transactions**

| Feature | State |
|---|---|
| Full ledger with filters | ⬜ |
| Inline category editing | ⬜ |
| Add transaction (modal) | ⬜ |
| Export CSV | ⬜ |

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
