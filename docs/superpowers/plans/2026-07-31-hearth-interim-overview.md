# Hearth Interim Overview (M2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `/` a real page — two of the design's eight Overview cards plus a setup checklist and a quick-create menu — so that every household, new or established, opens the app on something true instead of "Arriving in slice 5."

**Architecture:** Frontend composition only. No new endpoint, no migration, no domain change. `GET /accounts`, `GET /budgets/{month}` and `GET /household/members` already carry everything the page needs. Three private duplicates of the household-members query are collapsed into one shared hook first, because Overview would otherwise be the fourth copy. The page grows into the designed Overview when Bills, Goals, Marriage and Family exist — it is not replaced.

**Tech Stack:** React 19, TypeScript, TanStack Router 1.170, TanStack Query 5.101, Tailwind, Vitest + @testing-library/react.

**Spec:** `docs/superpowers/specs/2026-07-31-hearth-ux-repair-design.md` §4
**Depends on:** `docs/superpowers/plans/2026-07-31-hearth-ux-repair.md` (M1) being merged. The container from M1 Task 1 is what makes this page's card grid read correctly, and M1 Task 2 is what leaves the sidebar with only destinations that work.

## Global Constraints

- **Comments say why, never what.** `usecase/ports.go` is the model.
- **Every new test is mutation-checked**: break the code on purpose, watch the test go red, restore (`proving-tests-can-fail` skill).
- **One file, one job.** If describing a file needs the word "and", split it.
- **Fail closed on values you did not construct.** A `switch` over anything arriving from an API needs a `default` that refuses.
- **Money is minor units plus an ISO 4217 code.** `float64`/`number` arithmetic on a currency amount does not appear. Use the existing `formatMoney(amountMinor, currency, symbol)` from `features/money/formatMoney`.
- **Local calendar dates, never `toISOString()`.** This project has hit the UTC-conversion bug three times; `BudgetPage.tsx:55` and `AccountModal.tsx`'s `today()` both carry the comment explaining why.
- Test runner is `vitest`, driven with `fireEvent` — `@testing-library/user-event` is not a dependency.
- Definition of done: `make lint && make test` green, at least one new test mutation-checked, tracker/LEARNING/SYSTEM_DESIGN updated, and a browser walk covering three member states.

## Who can see what

This is the constraint most likely to be missed, because the seeded owner account never hits it. Overview is the **only** page every member reaches — Money's pages all sit behind `RequireCapability`.

| endpoint | guard (`api/internal/adapter/http/router.go`) | consequence for Overview |
|---|---|---|
| `GET /accounts` | `requireCapability(CapMoney)` | 403 for a member without money. With money but `role: limited`, it returns 200 **with `summary` omitted** — `schemas.ts:95-102` says that absence is the only signal for "this caller cannot see amounts", and the page must never synthesise one. |
| `GET /budgets/{month}` | `requireCapability(CapMoney)` **and** `requireOwner` (`router.go:186-199`) | 403 for anyone who is not an owner. The budget card is owner-only, not merely capability-gated. |
| `GET /household/members` | session only | available to everyone. |

So the page has three shapes:

- **owner** — net worth card, budget card, checklist, quick-add.
- **limited member with money** — net worth card showing account access but no figure; no budget card; no checklist; no quick-add.
- **limited member without money** — a single "You don't have access to Money" panel; no checklist; no quick-add.

The checklist is owner-only in all cases: a limited member can neither invite a member (`requireOwner` on `POST /household/members/invite`) nor write a budget, so offering the steps would be offering work they cannot do.

## File Structure

| file | change | responsibility |
|---|---|---|
| `web/src/features/settings/useHouseholdMembers.ts` | create | the one household-members query: its key, its fetch, its hook |
| `web/src/features/money/AccountModal.tsx` | modify | drops its private copy of that query |
| `web/src/features/money/TransactionsPage.tsx` | modify | drops its private copy of that query |
| `web/src/features/settings/MembersPanel.tsx` | modify | drops its private copy of that query |
| `web/src/features/money/month.ts` | create | `currentMonth()`, the local-calendar "YYYY-MM" |
| `web/src/features/money/BudgetPage.tsx` | modify | imports `currentMonth` instead of declaring it |
| `web/src/features/money/useBudget.ts` | modify | gains an `enabled` option, so a caller who may not read a budget can hold the request without breaking the rules of hooks |
| `web/src/features/overview/copy.ts` | create | every user-visible string on Overview |
| `web/src/features/overview/OverviewPage.tsx` | create | layout, data fetching, and which shape to render |
| `web/src/features/overview/BudgetCard.tsx` | create | this month's budget: percent used and the two figures behind it |
| `web/src/features/overview/SetupChecklist.tsx` | create | the four setup steps and their links |
| `web/src/features/overview/QuickAddMenu.tsx` | create | the "+ Add" menu and the two modals it opens |
| `web/src/features/overview/OverviewPage.test.tsx` | create | the three member shapes and the checklist's transitions |
| `web/src/routes/router.tsx` | modify | `/` renders `OverviewPage` |
| `web/src/features/placeholder/PlaceholderPage.tsx` | delete | Overview was its last user |

**No `overview/NetWorthCard.tsx`.** `features/money/NetWorthCard.tsx` already exists, already takes the `Summary` the accounts endpoint returns, and already handles the not-computable case. Overview imports it. The spec listed a new file here; reusing the real one is better and is what this plan does.

---

## Task 1: One household-members query, not four

`AccountModal.tsx:95-102`, `TransactionsPage.tsx:50-59` and `MembersPanel.tsx:31-39` each declare a byte-identical `fetchHouseholdMembers` + `useHouseholdMembers` pair against the same `["household", "members"]` key. Overview's checklist needs the same data. Extract before adding the fourth.

**Files:**
- Create: `web/src/features/settings/useHouseholdMembers.ts`
- Modify: `web/src/features/money/AccountModal.tsx`, `web/src/features/money/TransactionsPage.tsx`, `web/src/features/settings/MembersPanel.tsx`

**Interfaces:**
- Consumes: `membersListSchema`, `MemberView` from `features/settings/schemas` — `{ id, user, role, capabilities }`.
- Produces:
  - `householdMembersQueryKey: readonly ["household", "members"]`
  - `useHouseholdMembers(): UseQueryResult<MemberView[]>`

  Every later task and every existing caller imports these two names and no others.

**Why `features/settings/`.** `features/money/AccountModal.tsx:20` and `TransactionsPage.tsx:16` already import `membersListSchema` from `../settings/schemas`, so money reaching into settings for member types is the established direction. Putting the hook beside its schema keeps that direction unchanged rather than inventing a fourth feature directory for one file.

- [ ] **Step 1: Write the new module**

Create `web/src/features/settings/useHouseholdMembers.ts`:

```ts
// The household's member list, in one place. Three screens declared this same
// query privately -- AccountModal (to pick an account's owner), TransactionsPage
// (to filter by who paid) and MembersPanel (to list and edit them) -- against
// the same ["household", "members"] key, so they already shared a cache entry
// by coincidence rather than by construction. Overview's setup checklist would
// have been a fourth copy; this is that copy not being written.
//
// The key stays exactly ["household", "members"]: MembersPanel's mutations
// invalidate it by that literal, and changing it here would silently stop a
// role change from refreshing the list.
import { useQuery } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { membersListSchema, type MemberView } from "./schemas";

export const householdMembersQueryKey = ["household", "members"] as const;

async function fetchHouseholdMembers(): Promise<MemberView[]> {
  const body = await apiFetch<unknown>("/api/v1/household/members");
  return membersListSchema.parse(body);
}

export function useHouseholdMembers() {
  return useQuery({ queryKey: householdMembersQueryKey, queryFn: fetchHouseholdMembers });
}
```

- [ ] **Step 2: Point the three existing callers at it**

In each of `AccountModal.tsx`, `TransactionsPage.tsx` and `MembersPanel.tsx`: delete the local `householdMembersQueryKey` constant, the local `fetchHouseholdMembers` and the local `useHouseholdMembers`, and import instead.

`AccountModal.tsx` and `TransactionsPage.tsx`:

```ts
import { useHouseholdMembers } from "../settings/useHouseholdMembers";
```

`MembersPanel.tsx` (its local hook is named `useMembers`; rename the call sites):

```ts
import { householdMembersQueryKey, useHouseholdMembers } from "./useHouseholdMembers";
```

and replace its `membersQueryKey` references in the mutations' `invalidateQueries` with `householdMembersQueryKey`. If `useQuery` or `apiFetch` becomes unused in a file, remove the import — `make lint` will fail on it otherwise.

- [ ] **Step 3: Run the suites for all three callers**

Run: `cd web && npx vitest run src/features/money/AccountModal.test.tsx src/features/money/TransactionsPage.test.tsx src/features/settings/MembersPanel.test.tsx`
Expected: PASS, unchanged. This is a pure extraction — the request, the key and the parse are identical, so no existing test should move. A failure here means the extraction changed behaviour; find what, do not adjust the test.

- [ ] **Step 4: Type-check and lint**

Run: `make typecheck && make lint-web`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/settings/useHouseholdMembers.ts web/src/features/money/AccountModal.tsx web/src/features/money/TransactionsPage.tsx web/src/features/settings/MembersPanel.tsx
git commit -m "refactor(members): one household-members query instead of three copies

AccountModal, TransactionsPage and MembersPanel each declared the same
fetch and useQuery against the same ['household', 'members'] key, so they
shared a cache entry by coincidence rather than by construction. Overview's
setup checklist needs the same data and would have been the fourth."
```

---

## Task 2: `currentMonth()` where two screens can reach it

`BudgetPage.tsx:55` declares it privately, with the comment explaining why it must read the local calendar rather than `toISOString()`. Overview needs the same string for the same endpoint.

**Files:**
- Create: `web/src/features/money/month.ts`
- Modify: `web/src/features/money/BudgetPage.tsx`

**Interfaces:**
- Produces: `currentMonth(): string` — `"YYYY-MM"` in the local calendar.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/money/month.test.ts`:

```ts
// Pinned against toISOString(), not merely against "returns a string": a UTC
// conversion reads back the wrong month for any household east or west of UTC
// near a month boundary, which is the mistake this project has now made three
// times (see BudgetPage.tsx:52 and AccountModal.tsx's today()).
import { afterEach, describe, expect, it, vi } from "vitest";
import { currentMonth } from "./month";

afterEach(() => {
  vi.useRealTimers();
});

describe("currentMonth", () => {
  it("reads the local calendar month, not the UTC one", () => {
    // 1 August 2026, 07:00 in UTC+8 -- still 31 July in UTC. A household in
    // Singapore is in August; toISOString() would say July.
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-31T23:00:00.000Z"));
    const localMonth = `${new Date().getFullYear()}-${String(new Date().getMonth() + 1).padStart(2, "0")}`;

    expect(currentMonth()).toBe(localMonth);
  });

  it("pads a single-digit month", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2026, 2, 15));   // March, local
    expect(currentMonth()).toBe("2026-03");
  });
});
```

The first test derives its expectation from the same local getters rather than hardcoding "2026-08", so it passes wherever the suite runs while still failing against a `toISOString()` implementation.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && npx vitest run src/features/money/month.test.ts`
Expected: FAIL — "Failed to resolve import ./month".

- [ ] **Step 3: Write the module**

Create `web/src/features/money/month.ts`:

```ts
// Local calendar date, never toISOString() -- a UTC conversion can read back
// yesterday's (or tomorrow's) month for a household west or east of UTC, which
// is the same mistake the backend's dateOnly hit and AccountModal's today()
// guards against. Shared by BudgetPage and Overview, which both ask
// GET /budgets/{month} about "this month" and must agree on which month that is.
export function currentMonth(): string {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}`;
}
```

- [ ] **Step 4: Run it to verify it passes**

Run: `cd web && npx vitest run src/features/money/month.test.ts`
Expected: PASS, both cases.

- [ ] **Step 5: Point BudgetPage at it**

`web/src/features/money/BudgetPage.tsx` — delete the local `currentMonth` declaration and its comment (the comment moves to `month.ts`, above), and import:

```ts
import { currentMonth } from "./month";
```

`shiftMonth` stays in `BudgetPage.tsx`: it exists for the ‹ › picker, which is that screen's own control. Do not move it speculatively.

- [ ] **Step 6: Mutation-check**

Temporarily implement it the wrong way:

```ts
export function currentMonth(): string {
  return new Date().toISOString().slice(0, 7);   // MUTATION -- do not keep
}
```

Run: `cd web && npx vitest run src/features/money/month.test.ts`
Expected: FAIL on "reads the local calendar month". Restore and confirm green.

- [ ] **Step 7: Run the budget suite and commit**

Run: `cd web && npx vitest run src/features/money/BudgetPage.test.tsx`
Expected: PASS, unchanged.

```bash
git add web/src/features/money/month.ts web/src/features/money/month.test.ts web/src/features/money/BudgetPage.tsx
git commit -m "refactor(money): share currentMonth between Budget and Overview

Both ask GET /budgets/{month} about 'this month' and must agree on which
month that is. Its local-calendar reasoning now has a test rather than only
a comment."
```

---

## Task 3: Overview renders the two cards it can

The deliverable: `/` stops being a placeholder.

**Files:**
- Create: `web/src/features/overview/copy.ts`
- Create: `web/src/features/overview/BudgetCard.tsx`
- Create: `web/src/features/overview/OverviewPage.tsx`
- Create: `web/src/features/overview/OverviewPage.test.tsx`
- Modify: `web/src/routes/router.tsx`
- Delete: `web/src/features/placeholder/PlaceholderPage.tsx`

**Interfaces:**
- Consumes:
  - `useMe()` from `features/auth/useAuth` — `me.data.membership.role` is `"owner" | "limited"`, `me.data.capabilities` is `string[]`, `me.data.user.displayName` is a string.
  - `useCurrencies()` from `features/auth/useAuth` — `{ currencies: { code, symbol, name }[] }`.
  - `useAccounts(false)` from `features/money/useAccounts` — `{ accounts: Account[]; summary?: Summary }`.
  - `useBudget(month)` from `features/money/useBudget` — its `.query` holds `BudgetMonthResponse`: `{ currency, month, budget: Budget | null, budgetedMinor, spentMinor, remainingMinor, percentUsed, percentOk, daysLeft, … }`.
  - `NetWorthCard` from `features/money/NetWorthCard` — props `{ summary: Summary }`.
  - `formatMoney(amountMinor: number, currency: string, symbol?: string)` from `features/money/formatMoney`.
  - `currentMonth()` from `features/money/month` (Task 2).
- Produces: `OverviewPage` (no props), and `OVERVIEW_COPY`, consumed by Tasks 4 and 5.

- [ ] **Step 1: Write the copy module**

Create `web/src/features/overview/copy.ts`:

```ts
// Every user-visible string on Overview, in a plain .ts module for the same
// reason features/money/copy.ts is one: eslint's
// react-refresh/only-export-components rule never has to think about a file
// mixing components with other exports.
export const OVERVIEW_COPY = {
  title: "Overview",
  greeting: (name: string) => `Good to see you, ${name}.`,

  // Shown to a member whose capabilities do not include money. Not an error
  // and not an empty card: nothing is broken, this household has simply not
  // shared its money with them.
  noMoneyAccess: "You don't have access to Money in this household.",

  budgetHeading: "This month",
  // The never-budgeted state. The wording matches BudgetPage's own empty
  // state, which is the screen this card links to.
  budgetNone: "No budget set yet",
  budgetSetUp: "Set a budget",
  budgetUsed: (percent: number) => `${percent}% used`,
  budgetOf: (spent: string, budgeted: string) => `${spent} of ${budgeted}`,
} as const;
```

- [ ] **Step 2: Write the failing tests**

Create `web/src/features/overview/OverviewPage.test.tsx`. Follow `TransactionsPage.test.tsx`'s setup — `stubFetchRoutes` for every request, `renderWithRouter` because this page carries `<Link>`s:

```tsx
import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderWithRouter } from "../../test/renderWithRouter";
import { stubFetchRoutes, type RouteResponse } from "../../test/fetchStub";
import { currentMonth } from "../money/month";
import { OverviewPage } from "./OverviewPage";

const MONTH = currentMonth();

function meBody(overrides: { role?: string; capabilities?: string[] } = {}) {
  return {
    user: { id: "u1", email: "sam@newhouse.test", displayName: "Sam", avatarInitial: "S" },
    household: {
      id: "h1",
      name: "Rivera household",
      familyName: "Rivera",
      primaryCurrency: "SGD",
      showSecondaryCurrency: false,
      secondaryCurrency: "",
      fxRateMode: "static",
    },
    membership: {
      id: "m1",
      householdId: "h1",
      userId: "u1",
      role: overrides.role ?? "owner",
      capabilities: overrides.capabilities ?? ["calendar", "chores", "money", "marriage"],
    },
    capabilities: overrides.capabilities ?? ["calendar", "chores", "money", "marriage"],
    spaces: [],
  };
}

// A computable summary: the shape features/money/schemas.ts's discriminated
// union takes when the server could convert every account.
function summaryBody(netWorthMinor: number) {
  return {
    computable: true,
    currency: "SGD",
    netWorthMinor,
    assetsMinor: netWorthMinor,
    liabilitiesMinor: 0,
    excludedNoRate: [],
    excludedByChoice: 0,
  };
}

function budgetBody(overrides: Record<string, unknown> = {}) {
  return {
    currency: "SGD",
    month: MONTH,
    budget: { id: "b1", month: MONTH, expectedIncomeMinor: null },
    categories: [],
    budgetedMinor: 200000,
    spentMinor: 124000,
    remainingMinor: 76000,
    percentUsed: 62,
    percentOk: true,
    daysLeft: 2,
    dailyPaceMinor: 0,
    dailyPaceOk: true,
    byPerson: [],
    excludedNoRate: 0,
    overCount: 0,
    ...overrides,
  };
}

function renderOverview(routes: Record<string, RouteResponse>) {
  stubFetchRoutes({
    "GET /api/v1/currencies": {
      status: 200,
      body: { currencies: [{ code: "SGD", symbol: "S$", name: "Singapore dollar" }] },
    },
    "GET /api/v1/household/members": { status: 200, body: [] },
    ...routes,
  });
  return renderWithRouter(<OverviewPage />);
}

describe("OverviewPage", () => {
  it("shows net worth and this month's budget to an owner", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(1248000) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
    });

    expect(await screen.findByText("S$12,480.00")).toBeInTheDocument();
    expect(screen.getByText("62% used")).toBeInTheDocument();
  });

  it("tells a member without money that Money is not shared with them", async () => {
    renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ role: "limited", capabilities: ["calendar", "chores"] }),
      },
      "GET /api/v1/accounts": {
        status: 403,
        body: { error: { code: "FORBIDDEN", message: "Not allowed." } },
      },
    });

    expect(await screen.findByText(/don't have access to money/i)).toBeInTheDocument();
    // Not an error state, and not a zero -- zero would be a claim about this
    // household's money.
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("shows no budget card to a limited member, who cannot read one", async () => {
    // GET /budgets/{month} is requireCapability(money) AND requireOwner
    // (router.go). A limited member with money can see account names and
    // nothing else -- rendering a budget card that can only 403 would be a
    // card that is always broken.
    renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ role: "limited", capabilities: ["calendar", "chores", "money"] }),
      },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [] } },
    });

    await screen.findByText("Overview");
    expect(screen.queryByText("This month")).toBeNull();
  });

  it("offers a way to set one when the household has never budgeted", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: {
        status: 200,
        body: budgetBody({ budget: null, budgetedMinor: 0, spentMinor: 0, percentUsed: 0 }),
      },
    });

    const link = await screen.findByRole("link", { name: /set a budget/i });
    expect(link).toHaveAttribute("href", "/money/budget");
  });
});
```

Check `web/src/features/money/schemas.ts`'s `computableSummarySchema` before running — if its field names differ from `summaryBody` above, correct the fixture to match the schema, not the other way round.

- [ ] **Step 3: Run them to verify they fail**

Run: `cd web && npx vitest run src/features/overview/OverviewPage.test.tsx`
Expected: FAIL — "Failed to resolve import ./OverviewPage".

- [ ] **Step 4: Write BudgetCard**

Create `web/src/features/overview/BudgetCard.tsx`:

```tsx
// This month's budget, reduced to the one number a household glances at. The
// full screen is /money/budget; this card links there rather than repeating
// its category grid.
import { Link } from "@tanstack/react-router";
import { useCurrencies } from "../auth/useAuth";
import { formatMoney } from "../money/formatMoney";
import type { BudgetMonthResponse } from "../money/budgetSchemas";
import { OVERVIEW_COPY } from "./copy";

export function BudgetCard({ month }: { month: BudgetMonthResponse }) {
  const currencies = useCurrencies();
  const symbol = currencies.data?.currencies.find((c) => c.code === month.currency)?.symbol;

  return (
    <section
      aria-labelledby="overview-budget-heading"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <h2 id="overview-budget-heading" className="text-xs text-muted">
        {OVERVIEW_COPY.budgetHeading}
      </h2>

      {month.budget === null ? (
        <>
          <p className="mt-1.5 text-[15px] text-ink">{OVERVIEW_COPY.budgetNone}</p>
          <Link to="/money/budget" className="mt-3 text-[13px] font-semibold text-accent">
            {OVERVIEW_COPY.budgetSetUp}
          </Link>
        </>
      ) : (
        <>
          <p className="mt-1.5 text-[30px] font-semibold tracking-[-0.03em] text-ink">
            {OVERVIEW_COPY.budgetUsed(month.percentUsed)}
          </p>
          <p className="mt-1 text-[11.5px] text-muted">
            {OVERVIEW_COPY.budgetOf(
              formatMoney(month.spentMinor, month.currency, symbol),
              formatMoney(month.budgetedMinor, month.currency, symbol),
            )}
          </p>
        </>
      )}
    </section>
  );
}
```

- [ ] **Step 5: Let `useBudget` be told not to fire**

Overview must not ask for a budget on behalf of someone who may not read one. Calling the hook conditionally is not an option — that breaks the rules of hooks — and passing `""` as the month is worse than it looks: it sends `GET /api/v1/budgets/` for every limited member and leaves a failed query cached under `["budget", ""]`. The hook already uses `enabled` for its previous-month query; give the main one the same control.

`web/src/features/money/useBudget.ts`:

```ts
// `enabled` exists for Overview, which renders for every member but may only
// read a budget for an owner (GET /budgets/{month} is requireCapability(money)
// AND requireOwner). A caller cannot skip the hook -- that breaks the rules of
// hooks -- and passing a fake month would both fire a doomed request and cache
// a failure under a key nobody meant to write. Defaults to true, so BudgetPage
// is unaffected.
export function useBudget(month: string, options: { enabled?: boolean } = {}) {
  const queryClient = useQueryClient();
  const enabled = options.enabled ?? true;

  const query = useQuery({
    queryKey: budgetQueryKey(month),
    queryFn: () => fetchBudgetMonth(month),
    enabled,
  });
```

and the previous-month query's own gate becomes:

```ts
    enabled: enabled && query.data?.budget === null,
```

Leave every mutation in the hook alone — a mutation is only reached by a caller who already rendered the screen.

Run: `cd web && npx vitest run src/features/money/BudgetPage.test.tsx src/features/money/useBudget.test.ts`
Expected: PASS, unchanged. `enabled` defaults to true, so the existing caller's behaviour is identical.

- [ ] **Step 6: Write OverviewPage**

Create `web/src/features/overview/OverviewPage.tsx`:

```tsx
// The app's front door. Two of the design's eight cards -- the two Money can
// already supply -- rather than the placeholder that stood here, which every
// household saw on every visit, new and established alike.
//
// This page is the only one every member reaches: Money's pages sit behind
// RequireCapability, so a member without money never sees them. Which means
// the no-access shapes below are not an edge case, they are one of the three
// normal renders. See the guards in api/internal/adapter/http/router.go:
// /accounts needs the money capability; /budgets/{month} needs money AND
// owner.
import { useMe } from "../auth/useAuth";
import { NetWorthCard } from "../money/NetWorthCard";
import { currentMonth } from "../money/month";
import { useAccounts } from "../money/useAccounts";
import { useBudget } from "../money/useBudget";
import { BudgetCard } from "./BudgetCard";
import { OVERVIEW_COPY } from "./copy";

export function OverviewPage() {
  const me = useMe();
  const isOwner = me.data?.membership.role === "owner";
  const hasMoney = me.data?.capabilities.includes("money") ?? false;

  const accounts = useAccounts(false);
  // Only an owner may read a budget at all, so a limited member must not even
  // ask -- the request would be answered 403 and leave a failed query in the
  // cache for nobody to read. `enabled` (Step 5), not a fake month and not a
  // conditional call.
  const budget = useBudget(currentMonth(), { enabled: isOwner });

  return (
    <div className="flex flex-col gap-5 p-10">
      <h1 className="font-serif text-2xl">{OVERVIEW_COPY.title}</h1>

      {!hasMoney ? (
        <section className="rounded-xl border border-hairline bg-card p-[22px]">
          <p className="text-[13px] text-muted">{OVERVIEW_COPY.noMoneyAccess}</p>
        </section>
      ) : (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          {/* `summary` is omitted, not zeroed, for a member who may not see
              amounts (features/money/schemas.ts). Its absence is the only
              signal there is -- never synthesise one to fill the gap. */}
          {accounts.data?.summary && <NetWorthCard summary={accounts.data.summary} />}
          {isOwner && budget.query.data && <BudgetCard month={budget.query.data} />}
        </div>
      )}
    </div>
  );
}
```

Check `useBudget`'s return shape before writing this — if it returns the query directly rather than `{ query }`, adjust `budget.query.data` to match.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/overview/OverviewPage.test.tsx`
Expected: PASS, all four cases.

- [ ] **Step 8: Point `/` at it and delete the placeholder**

`web/src/routes/router.tsx`:

```tsx
import { OverviewPage } from "../features/overview/OverviewPage";
```

```tsx
const indexRoute = createRoute({
  getParentRoute: () => shellRoute,
  path: "/",
  component: OverviewPage,
});
```

Remove the `PlaceholderPage` import and update the file header comment's `/` line — it still says "(slice 5 placeholder)". Then:

```bash
git rm web/src/features/placeholder/PlaceholderPage.tsx
rmdir web/src/features/placeholder 2>/dev/null || true
```

M1 deleted the marriage and family routes, so Overview was this component's last user.

- [ ] **Step 9: Mutation-check**

Remove the `isOwner` guard on the budget card:

```tsx
          {budget.query.data && <BudgetCard month={budget.query.data} />}   // MUTATION
```

Run: `cd web && npx vitest run src/features/overview/OverviewPage.test.tsx -t "no budget card to a limited member"`
Expected: FAIL. Restore and confirm green.

- [ ] **Step 10: Full suite, type-check, commit**

Run: `make test-web && make typecheck && make lint-web`
Expected: PASS. `router.test.tsx` may assert the placeholder's text at `/` — update it to assert Overview's heading instead; the route's job did not change, only its component.

```bash
git add web/src/features/overview web/src/routes/router.tsx web/src/features/placeholder
git commit -m "feat(overview): give / a real page instead of a placeholder

Two of the design's eight cards -- net worth and this month's budget --
which are the two Money can supply today. Every household saw 'Arriving in
slice 5' on every visit until now, established ones included.

Overview is the only page every member reaches, so its no-access shapes are
normal renders, not edge cases: /accounts needs the money capability and
/budgets/{month} needs money and owner."
```

---

## Task 4: The setup checklist

Four steps, every one derived from data the page has already fetched. It disappears at four of four, so an established household does not carry a permanent chore list.

**Files:**
- Create: `web/src/features/overview/SetupChecklist.tsx`
- Modify: `web/src/features/overview/copy.ts`, `web/src/features/overview/OverviewPage.tsx`, `web/src/features/overview/OverviewPage.test.tsx`

**Interfaces:**
- Consumes: `useHouseholdMembers()` (Task 1), `accounts.data.accounts` and `budget.query.data` already fetched by `OverviewPage`.
- Produces: `SetupChecklist` with props `{ hasAccount: boolean; hasSecondMember: boolean; hasBudget: boolean }`. It renders nothing when all three are true.

**Why props and not its own fetching.** Three of the four steps read data `OverviewPage` already holds. A component that re-fetched them would double every request on the app's most-visited page and could disagree with the cards beside it about the same numbers.

**On "invite your partner".** An invited-but-unaccepted member is already a row in `GET /household/members` — `docs/HANDOVER.md` records the seeded household reading as "four members ... three accepted members plus Christine's deliberately pending invite". So `members.length > 1` covers both an accepted partner and a pending invite, and no new wire field is needed. If a walk shows a pending invite is *not* in that list, stop and add the field rather than shipping a step that stays unticked after inviting someone.

- [ ] **Step 1: Add the copy**

Append to `OVERVIEW_COPY` in `web/src/features/overview/copy.ts`:

```ts
  setupHeading: "Finish setting up",
  setupProgress: (done: number, total: number) => `${done} of ${total} done`,
  setupHousehold: "Create your household",
  setupAccount: "Add an account",
  setupPartner: "Invite your partner",
  // The month is read at render time, never written into this string -- a
  // literal "July" is wrong for eleven months of the year.
  setupBudget: (monthName: string) => `Set a budget for ${monthName}`,
  setupGo: "Set up",
```

- [ ] **Step 2: Write the failing tests**

Add to `web/src/features/overview/OverviewPage.test.tsx`:

```tsx
  it("shows a fresh household what is left to set up", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: {
        status: 200,
        body: budgetBody({ budget: null, budgetedMinor: 0, spentMinor: 0, percentUsed: 0 }),
      },
    });

    expect(await screen.findByText("Finish setting up")).toBeInTheDocument();
    expect(screen.getByText("1 of 4 done")).toBeInTheDocument();
  });

  it("drops the checklist once the household has finished setting up", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": {
        status: 200,
        body: {
          accounts: [
            {
              id: "a1",
              nickname: "DBS Everyday",
              type: "cash",
              ownerMembershipId: null,
              ownerName: null,
              balance: { amountMinor: 500000, currency: "SGD" },
              openingBalance: { amountMinor: 400000, currency: "SGD" },
              balanceAsOf: "2026-07-01",
              countTowardNetWorth: true,
              visibleToLimitedMembers: false,
              archivedAt: null,
            },
          ],
          summary: summaryBody(500000),
        },
      },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
      "GET /api/v1/household/members": {
        status: 200,
        body: [
          { id: "m1", user: { id: "u1", email: "sam@newhouse.test", displayName: "Sam", avatarInitial: "S" }, role: "owner", capabilities: ["money"] },
          { id: "m2", user: { id: "u2", email: "alex@newhouse.test", displayName: "Alex", avatarInitial: "A" }, role: "owner", capabilities: ["money"] },
        ],
      },
    });

    await screen.findByText("Overview");
    expect(screen.queryByText("Finish setting up")).toBeNull();
  });

  it("keeps the checklist away from a limited member, who cannot do any of it", async () => {
    // Inviting a member and writing a budget are both requireOwner. Offering
    // the steps would be offering work they cannot do.
    renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ role: "limited", capabilities: ["calendar", "chores", "money"] }),
      },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [] } },
    });

    await screen.findByText("Overview");
    expect(screen.queryByText("Finish setting up")).toBeNull();
  });
```

The account fixture is spelled out rather than imported from `TransactionsPage.test.tsx` — a test file that reaches into another feature's fixtures breaks when that feature's own tests change for reasons of their own.

- [ ] **Step 3: Run them to verify they fail**

Run: `cd web && npx vitest run src/features/overview/OverviewPage.test.tsx -t "what is left to set up"`
Expected: FAIL — no "Finish setting up" text.

- [ ] **Step 4: Write SetupChecklist**

Create `web/src/features/overview/SetupChecklist.tsx`:

```tsx
// What a household still has to do, on the page they land on. Renders nothing
// once every step is done, so an established household is not shown a
// permanent chore list.
//
// Takes its state as props rather than fetching: three of the four steps read
// data OverviewPage already holds, and a second fetch would both double the
// requests on the most-visited page and let this list disagree with the cards
// beside it about the same numbers.
import { Link } from "@tanstack/react-router";
import { OVERVIEW_COPY } from "./copy";

// Read at render time -- a household that opens the app in August must not be
// told to budget for July.
function monthName(): string {
  return new Date().toLocaleString(undefined, { month: "long" });
}

export function SetupChecklist({
  hasAccount,
  hasSecondMember,
  hasBudget,
}: {
  hasAccount: boolean;
  hasSecondMember: boolean;
  hasBudget: boolean;
}) {
  const steps = [
    // Always done: reaching this page at all required creating one. It is
    // listed anyway so the first thing a new household sees is something
    // already achieved rather than four things outstanding.
    { label: OVERVIEW_COPY.setupHousehold, done: true, to: null },
    { label: OVERVIEW_COPY.setupAccount, done: hasAccount, to: "/money" as const },
    { label: OVERVIEW_COPY.setupPartner, done: hasSecondMember, to: "/settings" as const },
    { label: OVERVIEW_COPY.setupBudget(monthName()), done: hasBudget, to: "/money/budget" as const },
  ];

  const done = steps.filter((s) => s.done).length;
  if (done === steps.length) return null;

  return (
    <section
      aria-labelledby="overview-setup-heading"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <div className="flex items-baseline justify-between">
        <h2 id="overview-setup-heading" className="text-sm font-semibold text-ink">
          {OVERVIEW_COPY.setupHeading}
        </h2>
        <span className="text-[11.5px] text-muted">
          {OVERVIEW_COPY.setupProgress(done, steps.length)}
        </span>
      </div>

      <ul className="mt-3 flex flex-col gap-2.5">
        {steps.map((step) => (
          <li key={step.label} className="flex items-center justify-between text-[13px]">
            <span className={step.done ? "text-muted line-through" : "text-ink"}>
              {step.done ? "✓ " : ""}
              {step.label}
            </span>
            {!step.done && step.to && (
              <Link to={step.to} className="text-[12.5px] font-semibold text-accent">
                {OVERVIEW_COPY.setupGo}
              </Link>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
```

- [ ] **Step 5: Wire it into OverviewPage**

In `web/src/features/overview/OverviewPage.tsx`, add the members query and render the checklist for an owner only:

```tsx
import { useHouseholdMembers } from "../settings/useHouseholdMembers";
import { SetupChecklist } from "./SetupChecklist";
```

```tsx
  const members = useHouseholdMembers();
```

and, inside the `hasMoney` branch, after the card grid:

```tsx
          {isOwner && (
            <SetupChecklist
              hasAccount={(accounts.data?.accounts.length ?? 0) > 0}
              // An invited-but-unaccepted member is already a row here (see
              // docs/HANDOVER.md on the seeded household's four members), so
              // this covers both an accepted partner and a pending invite.
              hasSecondMember={(members.data?.length ?? 0) > 1}
              hasBudget={budget.query.data?.budget != null}
            />
          )}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/overview/OverviewPage.test.tsx`
Expected: PASS, all seven cases.

- [ ] **Step 7: Mutation-check**

Make the checklist render unconditionally:

```tsx
  if (done === steps.length) return null;   // MUTATION: delete this line
```

Run: `cd web && npx vitest run src/features/overview/OverviewPage.test.tsx -t "drops the checklist"`
Expected: FAIL. Restore and confirm green.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/overview
git commit -m "feat(overview): show a new household what is left to set up

Four steps, each derived from data the page already fetched -- no new
endpoint. The block disappears once all four are done, so an established
household is not shown a permanent chore list, and it never renders for a
limited member, who cannot invite anyone or write a budget."
```

---

## Task 5: The "+ Add" quick-create menu

The design's menu offers Transaction, Account, Bill, Savings goal, Calendar event and Marriage retro. Two of those exist. It offers those two.

**Files:**
- Create: `web/src/features/overview/QuickAddMenu.tsx`
- Modify: `web/src/features/overview/copy.ts`, `web/src/features/overview/OverviewPage.tsx`, `web/src/features/overview/OverviewPage.test.tsx`

**Interfaces:**
- Consumes:
  - `AccountModal` from `features/money/AccountModal` — props `{ open: boolean; onClose: () => void; account?: Account }`. Self-contained: it fetches its own currencies and members and owns its create mutation.
  - `TransactionModal` from `features/money/TransactionModal` — props `{ open, onClose, onSubmit, onDelete?, initial?, accounts, members }`. Not self-contained: it needs the account list, the member list and a submit handler.
  - `useCreateTransaction()` from `features/money/useTransactions` — `mutateAsync(body: unknown): Promise<Transaction>`, invalidating the ledger on success.
  - `useHouseholdMembers()` (Task 1) and `useAccounts(false)`.
- Produces: `QuickAddMenu` with props `{ accounts: Account[] }`.

**Why only two entries.** `Sidebar.tsx` states the rule and this follows it: *"a permanent grey 'soon' row reads as broken."* A menu listing four things that do nothing is worse than a menu listing two that work.

**Owner-only.** `POST /transactions` and `POST /accounts` are both `requireOwner`. A limited member gets no menu, for the same reason they get no checklist.

- [ ] **Step 1: Add the copy**

Append to `OVERVIEW_COPY`:

```ts
  quickAdd: "+ Add",
  quickAddTransaction: "Transaction",
  quickAddAccount: "Account",
  // Transactions attach to an account. With none, the entry would open a
  // modal whose account dropdown is empty -- the dead end TransactionsPage's
  // own comment refuses.
  quickAddNeedsAccount: "Add an account first",
```

- [ ] **Step 2: Write the failing tests**

Add to `web/src/features/overview/OverviewPage.test.tsx`:

```tsx
  it("offers only the two things it can actually create", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
    });

    fireEvent.click(await screen.findByRole("button", { name: "+ Add" }));

    expect(screen.getByRole("button", { name: "Account" })).toBeInTheDocument();
    // Bill, Savings goal, Calendar event and Marriage retro are in the design
    // and do not exist. A row that does nothing reads as broken.
    expect(screen.queryByRole("button", { name: /bill/i })).toBeNull();
    expect(screen.queryByRole("button", { name: /savings goal/i })).toBeNull();
  });

  it("does not offer Transaction before there is an account to attach it to", async () => {
    renderOverview({
      "GET /api/v1/auth/me": { status: 200, body: meBody() },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [], summary: summaryBody(0) } },
      [`GET /api/v1/budgets/${MONTH}`]: { status: 200, body: budgetBody() },
    });

    fireEvent.click(await screen.findByRole("button", { name: "+ Add" }));

    expect(screen.getByRole("button", { name: "Transaction" })).toBeDisabled();
    expect(screen.getByText("Add an account first")).toBeInTheDocument();
  });

  it("gives a limited member no quick-add at all", async () => {
    renderOverview({
      "GET /api/v1/auth/me": {
        status: 200,
        body: meBody({ role: "limited", capabilities: ["calendar", "chores", "money"] }),
      },
      "GET /api/v1/accounts": { status: 200, body: { accounts: [] } },
    });

    await screen.findByText("Overview");
    expect(screen.queryByRole("button", { name: "+ Add" })).toBeNull();
  });
```

Add `fireEvent` to the file's `@testing-library/react` import.

- [ ] **Step 3: Run them to verify they fail**

Run: `cd web && npx vitest run src/features/overview/OverviewPage.test.tsx -t "only the two things"`
Expected: FAIL — no "+ Add" button.

- [ ] **Step 4: Write QuickAddMenu**

Create `web/src/features/overview/QuickAddMenu.tsx`:

```tsx
// The design's "+ Add" offers Transaction, Account, Bill, Savings goal,
// Calendar event and Marriage retro. Four of those features do not exist, and
// a permanently greyed row reads as broken rather than as a roadmap -- the
// same rule Sidebar.tsx's SPACE_PAGES states. Each entry joins this list in
// the change that builds the thing it creates.
//
// Rendered only for an owner: POST /transactions and POST /accounts are both
// requireOwner.
import { useState } from "react";
import { AccountModal } from "../money/AccountModal";
import { TransactionModal } from "../money/TransactionModal";
import { useCreateTransaction } from "../money/useTransactions";
import { useHouseholdMembers } from "../settings/useHouseholdMembers";
import type { Account } from "../money/schemas";
import { OVERVIEW_COPY } from "./copy";

export function QuickAddMenu({ accounts }: { accounts: Account[] }) {
  const [open, setOpen] = useState(false);
  const [accountOpen, setAccountOpen] = useState(false);
  const [transactionOpen, setTransactionOpen] = useState(false);
  const members = useHouseholdMembers();
  const createTransaction = useCreateTransaction();

  const canAddTransaction = accounts.length > 0;

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="rounded-lg bg-accent px-3.5 py-2 text-[13px] font-semibold text-white"
      >
        {OVERVIEW_COPY.quickAdd}
      </button>

      {open && (
        <div className="absolute right-0 z-10 mt-1.5 flex w-[220px] flex-col gap-0.5 rounded-xl border border-hairline bg-card p-1.5 shadow-[var(--shadow-auth-card)]">
          <button
            type="button"
            disabled={!canAddTransaction}
            onClick={() => {
              setOpen(false);
              setTransactionOpen(true);
            }}
            className="rounded-lg px-2.5 py-2 text-left text-[13px] text-ink disabled:cursor-not-allowed disabled:opacity-60"
          >
            {OVERVIEW_COPY.quickAddTransaction}
          </button>
          {/* Disabled with its reason beside it, not a modal whose account
              dropdown is empty -- the dead end TransactionsPage refuses. */}
          {!canAddTransaction && (
            <p className="px-2.5 pb-1 text-[11px] text-muted">
              {OVERVIEW_COPY.quickAddNeedsAccount}
            </p>
          )}

          <button
            type="button"
            onClick={() => {
              setOpen(false);
              setAccountOpen(true);
            }}
            className="rounded-lg px-2.5 py-2 text-left text-[13px] text-ink"
          >
            {OVERVIEW_COPY.quickAddAccount}
          </button>
        </div>
      )}

      <AccountModal open={accountOpen} onClose={() => setAccountOpen(false)} />
      <TransactionModal
        open={transactionOpen}
        onClose={() => setTransactionOpen(false)}
        onSubmit={(values) => createTransaction.mutateAsync(values)}
        accounts={accounts}
        members={(members.data ?? []).map((m) => ({ id: m.id, name: m.user.displayName }))}
      />
    </div>
  );
}
```

Check `TransactionModal`'s `onSubmit` parameter type (`TransactionFormValues`) and its `members` prop shape against the real file before running — if `members` wants a different field than `user.displayName`, follow the file, and if `TransactionsPage` maps it some other way, map it the same way here rather than inventing a second mapping.

- [ ] **Step 5: Wire it into OverviewPage**

In `OverviewPage.tsx`, put it beside the title and render it only for an owner:

```tsx
      <div className="flex items-center justify-between">
        <h1 className="font-serif text-2xl">{OVERVIEW_COPY.title}</h1>
        {isOwner && hasMoney && <QuickAddMenu accounts={accounts.data?.accounts ?? []} />}
      </div>
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/overview/OverviewPage.test.tsx`
Expected: PASS, all ten cases.

- [ ] **Step 7: Mutation-check**

Enable the Transaction entry unconditionally:

```tsx
  const canAddTransaction = true;   // MUTATION -- do not keep
```

Run: `cd web && npx vitest run src/features/overview/OverviewPage.test.tsx -t "before there is an account"`
Expected: FAIL. Restore and confirm green.

- [ ] **Step 8: Commit**

```bash
git add web/src/features/overview
git commit -m "feat(overview): quick-create for the two things that exist

The design's + Add offers six entries; Transaction and Account are the two
that are built, and the other four join this list in the change that builds
them. Transaction is disabled with its reason beside it until an account
exists, rather than opening a modal whose account dropdown is empty."
```

---

## Task 6: Verify in a real browser, then update the documents

**Files:**
- Create: `docs/superpowers/plans/2026-07-31-hearth-interim-overview-verification.md`
- Create: `docs/superpowers/plans/2026-07-31-hearth-interim-overview-screenshots/`
- Modify: `docs/FEATURE_TRACKER.md`, `docs/LEARNING.md`, `docs/SYSTEM_DESIGN.md`

- [ ] **Step 1: Run the full gate**

Run: `make lint && make test`
Expected: PASS. The Go suite needs a Docker socket:

```bash
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
```

- [ ] **Step 2: Walk a fresh household**

`make dev`. Sign up a new address, collect the mail at `http://localhost:8025`, complete step 2, and land on `/`.

1. Overview renders a heading, not "Arriving in slice 5."
2. The checklist reads **1 of 4 done**, with "Create your household" ticked.
3. "Add an account" links to `/money`. Follow it, add an account, come back: the checklist reads 2 of 4 and the net worth card shows the account's balance.
4. "+ Add" is present. Open it: Transaction is enabled now that an account exists; before adding the account it was disabled with "Add an account first" beside it.
5. Create a transaction from "+ Add". Confirm it appears at `/money/transactions` — the menu's mutation must invalidate the ledger, not just close.
6. "Set a budget for <this month>" names the current month, not a hardcoded one. Follow it, set a budget, come back: the checklist reads 3 of 4 and the budget card shows a percentage.
7. Invite a partner from Settings. Return to `/`: the checklist is 4 of 4 and therefore **gone**.

- [ ] **Step 3: Walk an established household**

`make seed`, sign in as the seeded owner, open `/`. Expected: no checklist at all, both cards populated with real figures, and the net worth figure equal to the one on `/money`. Two cards claiming different net worths would be the defect this walk exists to catch.

- [ ] **Step 4: Walk a limited member**

Create a limited member with the money capability:

```bash
make -n unlock-household >/dev/null    # confirm adminctl is reachable
docker compose exec api go run ./cmd/adminctl create-invite --capabilities=money
```

Accept the invite in a second browser profile (or after signing out), open `/`, and check: the net worth card shows no figure, there is no budget card, no checklist and no "+ Add". Nothing renders as an error.

Then repeat with a limited member with **no** money capability: `/` shows the single "You don't have access to Money in this household." panel and nothing else.

Check `adminctl`'s actual subcommands with `docker compose exec api go run ./cmd/adminctl --help` before running the above — use whatever it really offers rather than the sketch here.

- [ ] **Step 5: Capture the evidence**

Screenshots into `docs/superpowers/plans/2026-07-31-hearth-interim-overview-screenshots/`: fresh household at 1 of 4, the same household at 4 of 4 with the checklist gone, the seeded household, and both limited-member states. Two screenshots that come back byte-identical mean the state did not change — that exact failure is already recorded in `docs/LEARNING.md`.

- [ ] **Step 6: Write the verification file**

`docs/superpowers/plans/2026-07-31-hearth-interim-overview-verification.md`, in the shape `2026-07-30-hearth-budget-verification.md` uses: one numbered criterion per check in Steps 2–4, each marked pass or fail, with a note wherever a criterion was met by an interpreted path rather than a literal one.

- [ ] **Step 7: Update the three documents**

`docs/FEATURE_TRACKER.md` §4 (Overview):
- "Net worth card" ⬜ → 🟡 — named gap: the figure and account count only; the design's card carries more.
- "July budget card — percentage used" ⬜ → 🟡 — named gap: percentage and the two figures behind it; no sparkline.
- `"+ Add" quick-create menu` ⬜ → 🟡 — named gap: Transaction and Account only; Bill, Savings goal, Calendar event and Marriage retro join it when those features exist.
- Add a new row, **Setup checklist**, ✅, with a note that the design does not draw it — the tracker is the map of what exists, not only of what was drawn.
- The remaining five cards stay ⬜.
- Replace §4's "Nothing here is started. The page exists as a placeholder." with what is now true.
- Recount the summary table by the file's own stated rule — the first symbol in each row's cell — and confirm the columns sum.

`docs/LEARNING.md` — what this milestone taught:
- The same query was declared privately in three files against the same key, so they shared a cache entry by coincidence. It surfaced only when a fourth caller needed it. What would have caught it sooner: grepping for the endpoint before writing a hook for it.
- Overview is the only page every member reaches, which makes its no-access states normal renders rather than edge cases. Every other page in this app is behind a capability guard, so this was the first page where "what does a limited member see" was not answered by the router.
- `GET /budgets/{month}` is `requireCapability(money)` **and** `requireOwner`, which is deliberately unlike `/accounts`. A card built on the assumption that the two guards matched would 403 for every limited member.

`docs/SYSTEM_DESIGN.md` — use the `maintaining-system-design` skill. `/` gains a real component and three fetches; the placeholder component is gone. Update the route diagram, the frontend component diagram, and the prose beneath each — that prose is where the non-obvious reasoning lives.

- [ ] **Step 8: Commit**

```bash
git add docs/
git commit -m "docs: record the interim Overview walk and what it taught"
```

---

## Self-review

**Spec coverage.** §4.1 route and files → Task 3 Step 7 (route) and the File Structure table. §4.2 no new endpoints → stated and relied on throughout; no task adds one. §4.3 two cards → Task 3. §4.4 checklist → Task 4. §4.5 quick-create → Task 5. §4.6 limited member → the "Who can see what" table plus tests in Tasks 3, 4 and 5, and the walk in Task 6 Step 4. §4.7 definition of done → Task 6.

**Two deliberate departures from the spec, both improvements:**
1. The spec listed `overview/NetWorthCard.tsx`. `features/money/NetWorthCard.tsx` already exists and already handles the not-computable case; Overview imports it rather than growing a second one.
2. The spec described the budget card as capability-gated. `router.go:186-199` shows `/budgets/{month}` is `requireCapability(money)` **and** `requireOwner`, so the card is owner-only. The stricter rule is what this plan implements.

Tasks 1 and 2 are not in the spec at all — they are the extractions that stop Overview from being a fourth and second copy respectively. Both are the "targeted improvement to code you are working in" the design process calls for, and each is its own commit so a reviewer can take them separately.

**Placeholder scan.** None. Every code step carries its code, every test step its test, every run step its command and expected result. Three steps say "check the real file before running" — those are instructions to verify a signature this plan read but could not re-read at execution time, not deferred decisions.

**Type consistency.** `householdMembersQueryKey` and `useHouseholdMembers` are named identically in Task 1's module, Task 1's three call sites, Task 4's wiring and Task 5's menu. `OVERVIEW_COPY` gains keys in Tasks 3, 4 and 5 and every one is consumed under the name it was defined with. `SetupChecklist`'s three props are named identically in its definition and its single call site. `BudgetCard` takes `month: BudgetMonthResponse`, which is the exact type `useBudget`'s query resolves to.
