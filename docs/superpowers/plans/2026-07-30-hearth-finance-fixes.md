# Finance Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Same-day transactions move balances and net worth, the sidebar gains the design's grouped Money navigation, and the design household's names leave the product copy.

**Architecture:** Three small fixes on one branch, after the verified `worktree-transactions` branch merges to main. The balance rule changes in the SQL that computes it (sqlc-generated queries), the sidebar changes in one component, the copy changes in two components. No new routes, no schema changes, no new ports.

**Tech Stack:** Go + sqlc + pgx (testcontainers tests), React + TypeScript + vitest + testing-library, Tailwind.

**Spec:** `docs/superpowers/specs/2026-07-30-hearth-finance-fixes-design.md` — read it first; its four decisions are the contract.

## Global Constraints

- Money is `int64` minor units + ISO 4217 code; `float64` never in a monetary path.
- Authorisation lives in the HTTP layer only; nothing here touches guards.
- Every frontend test uses `stubFetchRoutes` (throws on unregistered requests).
- `make lint && make test` green before any task is called done; at least one new test mutation-checked (break the code, watch red, restore).
- Go suite needs Docker:
  `export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock`
  `export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock`
- sqlc queries are generated: edit `api/internal/adapter/postgres/queries/*.sql`, then `make sqlc` — never edit `sqlcgen/*.go` by hand.
- Rendered UI strings must not contain Andreas / Christine / Kayla / Ethan; comments may keep them.
- Work happens on branch `fix/finance-day-one` cut from main **after** Task 1's merge.

---

### Task 1: Merge the transactions branch, mark the known gap

**Files:**
- Modify: `docs/FEATURE_TRACKER.md` (Transactions section + summary table)

**Interfaces:**
- Produces: main contains the full Transactions feature; every later task's file paths exist on main.

- [ ] **Step 1: Preflight — confirm the worktree is clean and ahead**

```bash
cd /Volumes/Oink_Machine/Intelij/HouseholdDashboard
git -C .claude/worktrees/transactions status --short   # expect empty
git log --oneline main..worktree-transactions | wc -l  # expect ~41
```

- [ ] **Step 2: Merge (no fast-forward, matching the house merge style)**

```bash
git merge --no-ff worktree-transactions -m "Merge worktree-transactions: the household ledger, transfers and month spend"
```

If the merge conflicts, stop and report — do not resolve silently.

- [ ] **Step 3: Run the full suite on the merged tree**

Run: `make lint && make test` (with the Docker env vars above).
Expected: green. If red, stop and report.

- [ ] **Step 4: Mark the same-day gap in the tracker**

In `docs/FEATURE_TRACKER.md`, Transactions table, change:

```markdown
| Full ledger with filters | ✅ |
```

to:

```markdown
| Full ledger with filters | 🟡 — a transaction dated on its account's opening date does not move the balance or net worth, so nothing a household logs on day one changes any figure; fix specced in `docs/superpowers/specs/2026-07-30-hearth-finance-fixes-design.md` |
```

Recount the summary table at the top of the file (one ✅ becomes 🟡; the totals must sum).

- [ ] **Step 5: Commit and cut the fix branch**

```bash
git add docs/FEATURE_TRACKER.md
git commit -m "docs: mark the ledger's same-day gap the finance-fixes spec will close"
git checkout -b fix/finance-day-one
```

---

### Task 2: Same-day transactions count (backend rule change)

**Files:**
- Modify: `api/internal/adapter/postgres/queries/account.sql` (six `>` comparisons + the comment above the first)
- Modify: `api/internal/adapter/postgres/queries/transaction.sql` (six `<=` comparisons + the comment above GetTransaction)
- Modify: `api/internal/adapter/postgres/account_repo.go` (buildView doc comment)
- Regenerate: `api/internal/adapter/postgres/sqlcgen/` via `make sqlc`
- Test: `api/internal/adapter/postgres/account_repo_test.go`, `api/internal/adapter/postgres/transaction_repo_test.go`

**Interfaces:**
- Consumes: the merged Transactions schema (`accounts.opening_balance_as_of DATE`, `transactions.occurred_on DATE`).
- Produces: `AccountView.Balance` = opening balance + every transaction dated **on or after** `opening_balance_as_of`; wire flags `beforeFromAccountOpeningBalance`/`beforeToAccountOpeningBalance` are true only for **strictly before**. Task 8's browser walk relies on both.

- [ ] **Step 1: Flip the balance test to the new rule (write the failing test)**

In `account_repo_test.go`, `TestAccountBalanceSumsItsTransactions`: the account opens 10 July with 100000. Change the two same-day/before comments and the expectation — the 10 July expense (7777) now counts, the 3 July one (9999) still does not:

```go
	mustCreate(domain.TransactionExpense, 12, 5000, accountID, "")
	mustCreate(domain.TransactionIncome, 14, 20000, "", accountID)
	// Dated ON the opening date: the opening balance is the figure at the
	// START of that day (spec 2026-07-30, decision 1), so this counts.
	mustCreate(domain.TransactionExpense, 10, 7777, accountID, "")
	// Dated before it: already inside the opening figure, still excluded.
	mustCreate(domain.TransactionExpense, 3, 9999, accountID, "")
```

```go
	// 100000 - 5000 + 20000 - 7777
	if got := views[0].Balance.Amount; got != 107223 {
		t.Fatalf("balance = %d, want 107223 (opening 100000, -5000, +20000, -7777 on the "+
			"opening date itself, and nothing from the one dated before it)", got)
	}
```

- [ ] **Step 2: Flip the flag test, and pin the strictly-before boundary**

In `transaction_repo_test.go`, the "on the opening date" case (~line 127): the flag now must be **false** — and add a strictly-before case so the marker still has a red side. Replace the final assertion block with:

```go
	// Dated exactly ON the opening date: the opening balance is the balance
	// at the START of that day (spec 2026-07-30, decision 1), so this row
	// moves the balance and must NOT be marked.
	if onOpeningView.BeforeFromAccountOpening == nil || *onOpeningView.BeforeFromAccountOpening {
		t.Fatalf("beforeFromAccountOpening = %v, want non-nil false for a transaction on the opening date itself",
			onOpeningView.BeforeFromAccountOpening)
	}

	// Dated strictly BEFORE the opening date: still excluded, still marked.
	beforeOpening, err := repo.Create(ctx, domain.Transaction{
		HouseholdID:   householdID,
		Kind:          domain.TransactionExpense,
		OccurredOn:    time.Date(2026, time.June, 30, 0, 0, 0, 0, time.UTC),
		Description:   "Back-logged spend",
		FromAccountID: dbs,
		Amount:        domain.Money{Amount: 100, Currency: "SGD"},
	})
	if err != nil {
		t.Fatalf("create before opening date: %v", err)
	}
	beforeView, err := repo.Get(ctx, householdID, beforeOpening.ID)
	if err != nil {
		t.Fatalf("get before opening date: %v", err)
	}
	if beforeView.BeforeFromAccountOpening == nil || !*beforeView.BeforeFromAccountOpening {
		t.Fatalf("beforeFromAccountOpening = %v, want non-nil true for 30 June against a 1 July opening",
			beforeView.BeforeFromAccountOpening)
	}
```

(Keep the existing 18-July-wants-false assertion above it unchanged. Add the `time` import if it is not already there.)

- [ ] **Step 3: Run both tests to verify they fail**

Run: `cd api && go test ./internal/adapter/postgres/ -run 'TestAccountBalanceSumsItsTransactions|TestBeforeOpening|TestGetReports' -v` (use the actual name of the flag test's function — find it with `grep -n "onOpeningView" -B40 internal/adapter/postgres/transaction_repo_test.go | grep "^func"`).
Expected: FAIL — balance comes back 115000 (old rule), flag comes back true.

- [ ] **Step 4: Change the SQL rule**

In `queries/account.sql`: all **six** occurrences of

```sql
AND t.occurred_on > a.opening_balance_as_of
```

become

```sql
AND t.occurred_on >= a.opening_balance_as_of
```

(two each in `ListAccounts`, `ListAccountsIncludingArchived`, `GetAccount`). Rewrite the comment above the first sum:

```sql
       -- balance_minor is the opening balance plus every transaction dated
       -- ON OR AFTER opening_balance_as_of. The opening balance means the
       -- figure at the START of that day (spec 2026-07-30-hearth-finance-
       -- fixes, decision 1), so a transaction dated that same day counts —
       -- the day-one flow (create an account today, log today's dinner)
       -- must move the balance. A transaction dated strictly before stays
       -- out: that history is already inside the figure someone asserted.
```

In `queries/transaction.sql`: all **six** occurrences of

```sql
t.occurred_on <= fa.opening_balance_as_of
```
```sql
t.occurred_on <= ta.opening_balance_as_of
```

become `<` (three queries × two flags). Rewrite the `before_from_opening` paragraph of the file-top comment:

```sql
-- before_from_opening and before_to_opening are computed here, next to the
-- dates they compare, so the rule lives in one place. Strict < against
-- opening_balance_as_of, mirroring the >= the balance sum uses in
-- queries/account.sql: the opening balance is the figure at the START of
-- its day (spec 2026-07-30, decision 1), so only a transaction dated
-- strictly before it is already inside that figure and excluded.
```

In `account_repo.go`, `buildView`'s comment: "plus every transaction dated after opening_balance_as_of" becomes "plus every transaction dated on or after opening_balance_as_of", and "why the comparison is strict" becomes "why the comparison is >= (start-of-day rule)".

- [ ] **Step 5: Regenerate and run**

```bash
make sqlc
cd api && go test ./internal/adapter/postgres/ -v
```

Expected: PASS, including the untouched straddling-transfer test (its 20-July transfer is still after DBS's 1-July opening and still strictly before OCBC's 31-July one).

- [ ] **Step 6: Hunt siblings**

```bash
grep -rn "opening_balance_as_of" api/ --include="*.go" --include="*.sql" | grep -v sqlcgen | grep -v _test
grep -rn "dated after\|strict >" api/internal
```

Every comparison and every prose statement of the rule must say the new one. `usecase/monthsummary.go`'s "dated before its account's opening balance still counts here" is about month spend, not balance — it stays.

- [ ] **Step 7: Mutation-check both boundaries**

Revert ONE `>=` in `queries/account.sql` to `>`, `make sqlc`, run the balance test — must go RED. Restore. Revert ONE `<` in `queries/transaction.sql` to `<=`, `make sqlc`, run the flag test — must go RED. Restore, `make sqlc`, both green.

- [ ] **Step 8: Commit**

```bash
git add api/
git commit -m "fix(money): count same-day transactions — the opening balance is the start of its day"
```

---

### Task 3: Say the rule at the point of entry (account modal copy)

**Files:**
- Modify: `web/src/features/money/AccountModal.tsx` (~line 367, the "Starting balance as of" field)
- Test: `web/src/features/money/AccountModal.test.tsx`

**Interfaces:**
- Consumes: nothing new.
- Produces: the helper string `"The balance at the start of that day — transactions dated that day count."` (Task 8's walk greps for it).

- [ ] **Step 1: Write the failing test**

In `AccountModal.test.tsx` (reuse the file's `meFixture`, `CURRENCIES`, `NO_MEMBERS` fixtures and its render pattern — copy the arrangement from the "posts what the form was given" test):

```tsx
it("says the starting balance is the start-of-day figure", async () => {
  stubFetchRoutes({
    "GET /api/v1/auth/me": { status: 200, body: meFixture() },
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/household/members": NO_MEMBERS,
  });
  renderWithRouter(<AccountModal open onClose={() => {}} />);
  expect(
    await screen.findByText(
      "The balance at the start of that day — transactions dated that day count.",
    ),
  ).toBeInTheDocument();
});
```

(Match `AccountModal`'s actual props — check how the existing tests mount it and mirror that exactly.)

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/features/money/AccountModal.test.tsx`
Expected: FAIL — text not found.

- [ ] **Step 3: Add the helper line**

Under the as-of `<input>` (inside the same `flex flex-col gap-1.5` div):

```tsx
          <p className="text-[11.5px] leading-snug text-muted">
            The balance at the start of that day — transactions dated that day
            count.
          </p>
```

- [ ] **Step 4: Run the file's tests to verify they pass**

Run: `cd web && npx vitest run src/features/money/AccountModal.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/money/AccountModal.tsx web/src/features/money/AccountModal.test.tsx
git commit -m "fix(money): say the starting balance is a start-of-day figure where it is typed"
```

---

### Task 4: Grouped sidebar

**Files:**
- Modify: `web/src/features/shell/Sidebar.tsx`
- Modify: `docs/SYSTEM_DESIGN.md` (the two sidebar mentions, ~lines 613 and 868)
- Test: `web/src/features/shell/Sidebar.test.tsx`

**Interfaces:**
- Consumes: `me.spaces` (unchanged server contract — already filtered and ordered).
- Produces: sidebar shape — a space with multiple built pages renders as an uppercase group label plus one `<Link>` per page; a space with one page renders as today's single link; an unknown key renders as plain text. Money's pages: Finances `/money`, Transactions `/money/transactions`.

- [ ] **Step 1: Write the failing tests**

In `Sidebar.test.tsx`, using the file's existing `space()`/`meFixture()` helpers:

```tsx
it("groups Money into a label with Finances and Transactions links", async () => {
  stubFetchRoutes({});
  renderWithRouter(
    <Sidebar me={meFixture([space({ id: "space-money", key: "money", name: "Money", position: 1 })])} />,
  );
  // The group label is not a link.
  expect(screen.getByText("Money").closest("a")).toBeNull();
  expect(screen.getByRole("link", { name: "Finances" })).toHaveAttribute("href", "/money");
  expect(screen.getByRole("link", { name: "Transactions" })).toHaveAttribute(
    "href",
    "/money/transactions",
  );
});

it("keeps single-page spaces as one link, in payload order", async () => {
  stubFetchRoutes({});
  renderWithRouter(
    <Sidebar
      me={meFixture([
        space({ id: "space-money", key: "money", name: "Money", position: 1 }),
        space({ id: "space-marriage", key: "marriage", name: "Marriage", position: 2 }),
        space({ id: "space-family", key: "family", name: "Family", position: 3 }),
      ])}
    />,
  );
  expect(screen.getByRole("link", { name: "Marriage" })).toHaveAttribute("href", "/marriage");
  expect(screen.getByRole("link", { name: "Family" })).toHaveAttribute("href", "/family/calendar");
  // Order: the payload's, not alphabetical — Finances before Marriage before Family.
  const labels = screen.getAllByTestId("sidebar-space").map((el) => el.textContent);
  expect(labels).toEqual(["Money", "Finances", "Transactions", "Marriage", "Family"]);
});
```

Keep every existing test green; the unknown-key plain-text test must survive unchanged. Adjust the `data-testid="sidebar-space"` expectations above to however you attach testids — but each rendered space row (label, child link, or single link) must carry one so the order assertion can see them all.

- [ ] **Step 2: Run to verify the new tests fail**

Run: `cd web && npx vitest run src/features/shell/Sidebar.test.tsx`
Expected: the two new tests FAIL (no Finances/Transactions links exist).

- [ ] **Step 3: Implement the grouped rendering**

In `Sidebar.tsx`, replace `SPACE_PATHS` with:

```tsx
// One entry per built page of each space, in the design's order. A space
// with one entry renders as a single link named after the space; a space
// with several renders as the design's uppercase group label plus a link
// per page (the 5a sidebar). Budget, Goals and Bills get rows here only
// when their pages ship — a permanent grey "soon" row reads as broken.
const SPACE_PAGES: Record<string, { label: string; to: string }[]> = {
  money: [
    { label: "Finances", to: "/money" },
    { label: "Transactions", to: "/money/transactions" },
  ],
  marriage: [{ label: "Marriage", to: "/marriage" }],
  family: [{ label: "Family", to: "/family/calendar" }],
};
```

and rewrite `SpaceLink`:

```tsx
function SpaceLink({ space }: { space: Space }) {
  const pages = SPACE_PAGES[space.key];
  if (!pages) {
    return (
      <span data-testid="sidebar-space" className={`${NAV_ITEM_CLASS} text-muted`}>
        {space.name}
      </span>
    );
  }
  if (pages.length === 1) {
    return (
      <Link data-testid="sidebar-space" to={pages[0].to} className={NAV_ITEM_CLASS}>
        {space.name}
      </Link>
    );
  }
  return (
    <>
      <div
        data-testid="sidebar-space"
        className="px-2.5 pb-1 pt-3.5 text-[10.5px] font-semibold uppercase tracking-[0.09em] text-muted"
      >
        {space.name}
      </div>
      {pages.map((page) => (
        <Link
          key={page.to}
          data-testid="sidebar-space"
          to={page.to}
          className={NAV_ITEM_CLASS}
          activeProps={{ className: `${NAV_ITEM_CLASS} text-accent` }}
          activeOptions={{ exact: page.to === "/money" }}
        >
          {page.label}
        </Link>
      ))}
    </>
  );
}
```

`activeOptions.exact` on `/money` is load-bearing: without it, `/money/transactions` marks both links active (TanStack Router's default prefix matching). Update the component's header comment — the "sub-pages belong to slices 2–4, which haven't been built yet" paragraph is no longer true; say the grouped form arrived with Transactions and the map grows a row per shipped page.

- [ ] **Step 4: Run the suite for the file, then the whole web suite**

Run: `cd web && npx vitest run src/features/shell/Sidebar.test.tsx && npx vitest run`
Expected: PASS everywhere (AppShell tests render the sidebar too — fix any that pinned the old flat shape).

- [ ] **Step 5: Verify the active-state in the real browser**

With the dev stack up (`make dev` or already running): open http://localhost:5173/money — Finances link accented, Transactions not; open /money/transactions — reversed. jsdom cannot check this (router active state it can, visual accent it cannot) — look at it.

- [ ] **Step 6: Update SYSTEM_DESIGN's sidebar prose**

Line ~613's box label and line ~868's "The sidebar is untouched" paragraph: the sidebar still renders from `me.spaces` (server-filtered, server-ordered), but a space now expands into its built pages via a client-side map — Finances and Transactions under Money today. Keep the sentence that the sidebar never filters client-side.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/shell/ docs/SYSTEM_DESIGN.md
git commit -m "feat(shell): the design's grouped sidebar — Money expands to Finances and Transactions"
```

---

### Task 5: The kids-visibility helper names the real household

**Files:**
- Modify: `web/src/features/money/AccountModal.tsx` (~lines 396–404)
- Test: `web/src/features/money/AccountModal.test.tsx`

**Interfaces:**
- Consumes: `useHouseholdMembers()` already in the component (`members.data`, `MemberView[]` — `role`, `user.displayName`).
- Produces: helper copy — real limited-member names, or the generic line.

- [ ] **Step 1: Write the failing tests**

```tsx
it("names the household's limited members under Visible to kids", async () => {
  stubFetchRoutes({
    "GET /api/v1/auth/me": { status: 200, body: meFixture() },
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/household/members": {
      status: 200,
      body: [
        { id: "m1", role: "owner", capabilities: ["money"], user: { id: "u1", email: "", displayName: "Dre", avatarInitial: "D" } },
        { id: "m2", role: "limited", capabilities: [], user: { id: "u2", email: "", displayName: "Kayla", avatarInitial: "K" } },
        { id: "m3", role: "limited", capabilities: [], user: { id: "u3", email: "", displayName: "Ethan", avatarInitial: "E" } },
      ],
    },
  });
  renderWithRouter(<AccountModal open onClose={() => {}} />);
  expect(
    await screen.findByText("Kayla & Ethan can see this account exists, not the balance"),
  ).toBeInTheDocument();
});

it("falls back to generic copy when no member is limited", async () => {
  stubFetchRoutes({
    "GET /api/v1/auth/me": { status: 200, body: meFixture() },
    "GET /api/v1/currencies": CURRENCIES,
    "GET /api/v1/household/members": NO_MEMBERS,
  });
  renderWithRouter(<AccountModal open onClose={() => {}} />);
  expect(
    await screen.findByText("Limited members can see this account exists, not the balance"),
  ).toBeInTheDocument();
});
```

(Mirror the member-fixture shape the file's existing tests use for the owner dropdown — if they already have a members fixture, extend it rather than inventing a second shape. Match `userSchema`'s exact fields.)

- [ ] **Step 2: Run to verify the first fails**

Run: `cd web && npx vitest run src/features/money/AccountModal.test.tsx`
Expected: the seeded-names test FAILS against fixtures whose limited members are named differently… it may PASS by coincidence if your fixture uses Kayla/Ethan — so also assert the hardcoded string is gone by running the fallback test, which FAILS while the literal string renders.

- [ ] **Step 3: Implement**

Replace the hardcoded helper div (and its "Literal design copy" comment) with:

```tsx
            <div className="mt-0.5 text-[11.5px] text-muted">
              {limitedMembersLine(members.data)}
            </div>
```

and add, near the top of the file (after the schema imports):

```tsx
// The design wrote "Kayla & Ethan can see this account exists, not the
// balance" — true only of the seeded household. Real households get their
// own limited members' names, and a household with none gets the generic
// line rather than an invented family.
function limitedMembersLine(members: MemberView[] | undefined): string {
  const names = (members ?? [])
    .filter((m) => m.role === "limited")
    .map((m) => m.user.displayName);
  if (names.length === 0) {
    return "Limited members can see this account exists, not the balance";
  }
  const list =
    names.length === 1
      ? names[0]
      : `${names.slice(0, -1).join(", ")} & ${names[names.length - 1]}`;
  return `${list} can see this account exists, not the balance`;
}
```

- [ ] **Step 4: Run the file's tests to verify they pass**

Run: `cd web && npx vitest run src/features/money/AccountModal.test.tsx`
Expected: PASS.

- [ ] **Step 5: Mutation-check the filter**

Change `m.role === "limited"` to `m.role !== "limited"` — the names test must go RED (it would say "Dre can see…"). Restore, green.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/money/AccountModal.tsx web/src/features/money/AccountModal.test.tsx
git commit -m "fix(money): the kids-visibility helper names the real limited members, not the design's"
```

---

### Task 6: Generic currency-panel copy, and the sweep

**Files:**
- Modify: `web/src/features/settings/CurrencyPanel.tsx` (~lines 170–179)
- Test: `web/src/features/settings/CurrencyPanel.test.tsx` if it exists; otherwise the sweep below is the check

**Interfaces:**
- Consumes: nothing new.
- Produces: no rendered design-household name anywhere in `web/src`.

- [ ] **Step 1: Replace the literal copy**

In `CurrencyPanel.tsx`, delete the "Literal design copy…" comment block and change:

```tsx
              <div className="mt-0.5 text-[11.5px] text-muted">
                For Christine's Indonesian accounts
              </div>
```

to:

```tsx
              <div className="mt-0.5 text-[11.5px] text-muted">
                Shown alongside the primary currency
              </div>
```

- [ ] **Step 2: Sweep for rendered design names**

```bash
cd web && grep -rn "Andreas\|Christine\|Kayla\|Ethan" src --include="*.tsx" --include="*.ts" | grep -v "\.test\." | grep -v "^\s*//" 
```

Read every hit in context: comments stay; anything inside JSX or a rendered string is a defect — fix it the same way (real data or generic copy) and note it in the commit message. Then build and grep the bundle:

```bash
npx vite build && grep -o "Kayla\|Ethan\|Christine" dist/assets/*.js | sort | uniq -c
```

Expected: no output. (`meFixture`-style names in test files never reach the bundle.)

- [ ] **Step 3: Run the web suite**

Run: `cd web && npx vitest run`
Expected: PASS — if a CurrencyPanel test pinned the old string, flip it to the new one.

- [ ] **Step 4: Commit**

```bash
git add web/src
git commit -m "fix(settings): generic secondary-currency copy — Christine belongs to the seed, not the product"
```

---

### Task 7: Docs — tracker, learning

**Files:**
- Modify: `docs/FEATURE_TRACKER.md`
- Modify: `docs/LEARNING.md`

**Interfaces:**
- Consumes: Tasks 2–6 landed.
- Produces: docs that tell the truth about them.

- [ ] **Step 1: Tracker**

- The Transactions "Full ledger with filters" row: 🟡 back to ✅ (the gap Task 1 named is closed).
- Add a row for the grouped sidebar under whichever section holds shell/navigation features — if none covers it, add it as a new row and count it.
- Recount the summary table; columns must sum.

- [ ] **Step 2: Learning entry**

Add one entry (and attach the copy instance to an existing hardcoding pattern if one fits, per the file's own convention):

- **What broke:** a 15-of-15 verified feature still failed the owner's first real day — same-day transactions moved nothing, Transactions had no visible navigation, and the modal spoke of children from the design's example family.
- **Symptom:** "I added an expense and net worth didn't move"; "how do I get to transactions?"; "who is Ethan?"
- **What would have caught it sooner:** walking the product as a day-one stranger, not only as the spec's own criteria — every criterion was derived from the spec, and the spec was the thing that was wrong (decision 6), silent (navigation), or unexamined (copy). Now in CLAUDE.md's definition of done: real-browser testing, as the user.

- [ ] **Step 3: Commit**

```bash
git add docs/FEATURE_TRACKER.md docs/LEARNING.md
git commit -m "docs: record what the day-one walk taught, and tick the ledger back to done"
```

---

### Task 8: Definition-of-done browser walk

**Files:**
- Create: `docs/superpowers/plans/2026-07-30-hearth-finance-fixes-verification.md`

**Interfaces:**
- Consumes: everything above, running via `make dev` on the fix branch.

- [ ] **Step 1: Reset to a clean stack**

```bash
make down
docker volume rm hearth_hearth-pgdata
make up && make seed
```

(Check `docker ps` on both engines first — the two-engine trap in `docs/LEARNING.md`.)

- [ ] **Step 2: Walk the five criteria in a real browser** (Claude in Chrome / Playwright — click it, do not curl it)

1. Sign up a **fresh** household (not the seed). Create an account dated today, log an expense — Finances balance and net worth drop by exactly the amount, on screen, without a manual reload.
2. Log income — both rise by exactly the amount.
3. Log an expense dated before the account's opening date — excluded, and the ledger row is marked with the account's name.
4. Sidebar shows the MONEY group with Finances and Transactions; both navigate; the accent follows the route (exact-match on Finances — /money/transactions must not accent both).
5. Add-account modal shows the generic limited-members line on this household (it has none); in the seeded household it names the seeded children. No design-household name appears anywhere clicked through (Money, Settings).

- [ ] **Step 3: Record the walk**

Write the verification file in the house style (see `2026-07-29-hearth-transactions-verification.md`): each criterion, pass/fail, what was actually seen, screenshots beside it if taken. A failure is fixed and re-walked, and the record says so plainly.

- [ ] **Step 4: Full suite, then commit**

```bash
make lint && make test
git add docs/superpowers/plans/2026-07-30-hearth-finance-fixes-verification.md
git commit -m "docs: record the finance fixes' day-one walk"
```

---

## After the plan

Merge `fix/finance-day-one` back to main per `superpowers:finishing-a-development-branch`. The tracker flip in Task 7 assumed the merge completes — do not leave the branch unmerged with the tracker claiming ✅.
