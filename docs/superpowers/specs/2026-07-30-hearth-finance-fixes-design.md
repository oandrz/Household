# Finance fixes — same-day balance rule, grouped sidebar, honest copy

Written 2026-07-30, from the product owner's first real day-one use of the
Transactions build. Three defects, each reproduced in a real browser against
the running stack before this spec was written. All three land on one branch,
**after** the verified `transactions` worktree branch merges to main.

The owner's report, verbatim in spirit:

1. Logging an expense of 100 did not reduce net worth; logging income did not
   raise it. Expectation: any transaction moves net worth.
2. No visible navigation to the Transactions screen — how does a user get
   there?
3. The Add-account modal mentions "Ethan", who is not a member of the owner's
   household. Is this hardcoded?

Each turned out real, and none was caught by the 15-of-15 verification walk —
which is itself a lesson (see "What this teaches", below).

---

## What was found

### 1. Same-day transactions never move the balance

Transactions spec decision 6 defined `AccountView.Balance` as the opening
balance plus every transaction dated **strictly after**
`opening_balance_as_of`, on the theory that a balance typed "as of" a date
already reflects that day's spending. The implementation matches the spec
exactly (`account_repo.go`, both sum sites: `t.occurred_on >
a.opening_balance_as_of`).

The theory fails on the most natural flow in the product: a new user creates
an account **today**, with today's date as the default "as of", then logs
today's dinner. The transaction saves, appears in the ledger — marked "Before
UOB's opening balance — it doesn't change that balance", copy that is wrong
twice over (it is *same-day*, not before, and the user never asked for this
exclusion) — and the balance and net worth sit still. Every day-one user hits
this; the walk did not, because its criteria were scripted from the spec that
made the mistake.

### 2. Transactions is reachable only through a small "See all" link

`/money/transactions` exists and works. Its only entry point is the "See all"
link beside "Recent transactions" on the Finances page. The design's sidebar
is explicit and grouped:

```
Overview
MONEY          ← uppercase group label
  Finances
  Transactions
  Budget
  Goals
  Bills
MARRIAGE
  Retros
  Vision & goals
  Agreements
FAMILY
  Calendar
```

`Sidebar.tsx` deliberately deferred grouping "until a space has more than one
destination" (its own header comment). Transactions shipped; the condition
has expired.

### 3. The design household's names shipped as product copy

"Kayla & Ethan can see this account exists, not the balance" is a hardcoded
string under the Add-account modal's "Visible to kids" toggle
(`AccountModal.tsx`). Sibling, found by hunting the class: "For Christine's
Indonesian accounts" in the Settings currency panel (`CurrencyPanel.tsx`) —
its own comment even flags it as "literal design copy … flagged in the
report, not solved here". Both were harmless while the seeded household was
the only household; self-serve sign-up made them wrong for every real
customer. The owner-dropdown itself is clean — it renders the real member
list.

---

## Decisions

### Decision 1 — the opening balance is the balance at the *start* of its day

`opening_balance_as_of` changes meaning from end-of-day to start-of-day: a
transaction dated **on** the as-of date now counts toward the balance, so the
day-one flow works — log an expense today, net worth drops now.

The trade-off, accepted with eyes open: someone who types tonight's
end-of-day bank figure and then back-logs today's already-included expense
will double-count it. Copy carries the meaning at the point of entry — the
account modal's "Starting balance as of" field says the balance is as of the
**start** of that day — and the alternative (keeping strict-after and
warning at transaction entry) leaves the most natural first-run flow
silently broken, which is the worse failure. This is how ledger tools users
already know behave.

Strictly-before transactions stay excluded and stay marked in the ledger —
that half of the old decision 6 (importing history must not corrupt a
balance) was right and survives unchanged.

### Decision 2 — the sidebar groups, showing only what exists

The sidebar takes the design's grouped form now: each space renders as an
uppercase group label plus child page links when it has more than one built
destination, and as today's single link when it has one. The child list is a
client-side map keyed by space key — the server's `me.spaces` contract does
not change, and no route changes. Money's children today: Finances
(`/money`), Transactions (`/money/transactions`). Budget, Goals and Bills
rows appear only when each ships — a permanent grey "soon" row reads as
broken, so unbuilt pages get no row at all.

### Decision 3 — copy names real members where the data is in hand, and goes generic where it is not

The Add-account modal already fetches the member list for its owner
dropdown, so the kids-visibility helper names the household's actual limited
members ("Dre can see this account exists, not the balance"; names joined
with commas and "&"), falling back to "Limited members can see this account
exists, not the balance" when there are none. The currency panel has no
per-member account data to be personal with, so it gets honest generic copy:
"Shown alongside the primary currency". A sweep for any other rendered
design-literal name (Andreas, Christine, Kayla, Ethan) is part of the work —
comments may keep the names; rendered strings may not.

### Decision 4 — merge the transactions branch first, then fix

The `transactions` worktree branch (41 commits, walked 15 of 15, docs
current) merges to main as-is; these fixes land on a fresh branch on top.
Reopening a verified branch to mix in a spec change would blur what the
15-of-15 claim covered. At merge time the feature tracker marks the ledger 🟡
with the same-day gap named, so the tracker never claims a state the product
does not have; this branch turns it back to ✅.

---

## The changes

### Backend

- `account_repo.go`: both balance-sum comparisons become `t.occurred_on >=
  a.opening_balance_as_of`.
- Every other comparison against `opening_balance_as_of` is audited in the
  same change (hunting siblings, not the instance). Known today: the wire
  flags `beforeFromAccountOpeningBalance` / `beforeToAccountOpeningBalance`
  must mean strictly-before (`<`), so a same-day row is no longer marked as
  not counting.
- Doc comments that state the old rule (`ports.go`,
  `AccountRepository.List`, domain comments) restate the new one.

### Frontend

- `Sidebar.tsx`: grouped rendering per decision 2, driven by a
  `SPACE_PAGES` map replacing `SPACE_PATHS`. Active-link styling per child.
  Spaces with an unrecognised key keep the current plain-text fallback.
- `AccountModal.tsx`: kids-visibility helper per decision 3.
- `CurrencyPanel.tsx`: generic secondary-currency copy per decision 3.
- Ledger row marker: appears only for strictly-before rows (falls out of the
  backend flag change; the frontend renders the flag it is given).

### Tests

- Existing repository tests that pin `>` flip to pin `>=`, mutation-checked
  both ways: reverting `>=` to `>` must go red, and so must the before-flag
  boundary (a same-day row claiming `beforeFromAccountOpeningBalance:
  true`).
- Sidebar tests: grouped shape, active states, single-link spaces untouched,
  unknown-key fallback.
- AccountModal test: helper names the fixture's limited members; fallback
  when none.
- Frontend tests keep using `stubFetchRoutes` throughout.

### Browser walk (definition of done, run by the implementer in a real browser)

1. Fresh household, create account with today's date, log an expense — the
   Finances balance and net worth drop by exactly the amount, on screen,
   without a reload.
2. Log income — both rise by exactly the amount.
3. A transaction dated before the account's opening date stays excluded and
   marked.
4. Sidebar shows MONEY with Finances and Transactions links; both navigate;
   active state follows the route.
5. Add-account modal names the household's real limited members, or the
   generic line when there are none; no design-household name appears
   anywhere in the rendered app (grep the built bundle, then click through
   Settings and Money).

### Docs

- `FEATURE_TRACKER.md`: ledger row 🟡→✅ once the same-day rule lands; a row
  for the grouped sidebar if none covers it; summary table recounted.
- `LEARNING.md`: one entry — *a verification walk scripted from the spec
  cannot catch the spec's own wrong decision; walk the user's most natural
  flow too, not only the criteria* — plus the design-copy-as-product-copy
  instance added to the existing hardcoding pattern if one exists, or as a
  new pattern if not.
- `SYSTEM_DESIGN.md`: only if it draws the flat sidebar; update the drawing
  and its prose together.

---

## What this teaches

The 15-of-15 walk was real, in a real browser, on a wiped database — and
still missed all three of these, because every criterion was derived from
the spec and the spec was the thing that was wrong (decision 6), silent
(navigation), or unexamined (copy). The check that catches this class is
using the product the way a stranger would on day one, which is now recorded
in `CLAUDE.md`'s definition of done: test the product in a real browser
before calling it done — as the user, not as the spec.
