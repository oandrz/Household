# Handover — M2, the interim Overview

Written 2026-07-31 by the agent that ran M1. Read this before you touch
anything. It is the context that is not in the plan: the decisions already
taken with the product owner, the things M1 learned the hard way, and the
facts about this codebase that cost real time to discover.

Your deliverable is the plan. This document exists so you do not re-derive what
is already settled, and do not step on the four traps that caught M1.

---

## 1 · Where things stand

**M1 is complete and in review.** Branch `fix/ux-repair`, pull request
[#4](https://github.com/oandrz/Household/pull/4), 15 commits. `make lint &&
make test` green — 10 Go packages, 28 frontend files, 278 tests.

It shipped no feature. It repaired four things a UI/UX critique found:

- `AppShell` now bounds page content to **1204px** (the design's 1440px canvas
  minus its 236px sidebar). Before this, a page heading and its primary action
  measured 2407px apart on the owner's monitor.
- Marriage and Family left the sidebar; their four routes are deleted.
- The empty ledger offers an account-first state linking to `/money`.
- Sign-up and sign-in describe a family (parents plus kids), matching the
  domain, instead of claiming a two-person product.

**M2 is yours.** Plan:
`docs/superpowers/plans/2026-07-31-hearth-interim-overview.md`. Spec §4:
`docs/superpowers/specs/2026-07-31-hearth-ux-repair-design.md`.

**Start from `fix/ux-repair`, or from `main` once #4 merges.** M2 depends on
M1: the plan's card grid assumes the 1204px container exists, and it deletes
`PlaceholderPage`, whose last remaining user is `/` only because M1 removed the
other three.

---

## 2 · Decisions already taken — do not re-open these

The product owner settled four questions on 2026-07-31, each with the
alternatives on the table. They are recorded in the spec's §2, and re-litigating
them wastes their time.

**The front door is an interim Overview built only on Money.** The designed
Overview is eight cards aggregating Money, Marriage and Family, and
`FEATURE_TRACKER.md`'s own suggested order puts it last for that reason. Two of
the eight are buildable today. The alternatives — redirect `/` to Finances, or
show only a first-run checklist — were rejected because an established
household still gets no orientation. The same route and the same component grow
into the designed Overview later; they are not replaced.

**Hearth is a family product: parents plus kids.** The domain already said so.
The sign-up copy was what changed, in M1.

**Two milestones, not one branch.** M2 gets its own branch, its own review, its
own browser walk.

**Unbuilt things stay out of the UI.** `Sidebar.tsx` states the rule: "a
permanent grey 'soon' row reads as broken." M2 applies it to the "+ Add" menu —
four of the design's six entries do not exist, so the menu offers two.

---

## 3 · Four facts about this codebase that will cost you time

Every one of these was discovered during M1 by reading source, and each
invalidates something a reasonable person would assume.

### 3.1 `/budgets/{month}` needs money **and** owner

`api/internal/adapter/http/router.go:186-199`. The transactions group is
`requireCapability(CapMoney)` **and** `requireOwner`, deliberately unlike the
accounts group above it, and the router says why in a comment.

So Overview's budget card is **owner-only**, not merely capability-gated. The
spec was written before this was known and describes it as capability-gated; the
plan is correct and the plan wins.

### 3.2 A limited member's accounts response omits `summary` entirely

`web/src/features/money/schemas.ts:95-102` — `summary` is `.optional()`, and its
comment is load-bearing:

> summary is absent entirely for a limited member — the server omits it rather
> than sending a zeroed one, and that absence is the one signal the frontend has
> for "this caller cannot see amounts." The page must never synthesise a summary
> to fill this gap.

Overview is **the only page every member reaches** — every Money page sits
behind `RequireCapability`. So the no-access renders are one of three normal
shapes, not an edge case, and the seeded owner account will never show them to
you. Walk them deliberately.

### 3.3 `useBudget` cannot be called conditionally, and must not be given a fake month

The plan's Task 3 Step 5 adds an `enabled` option to `useBudget`. Do not skip it
and do not "simplify" it to `useBudget(isOwner ? currentMonth() : "")` — that
fires `GET /api/v1/budgets/` for every limited member and caches a failure under
`["budget", ""]`. The hook already uses `enabled` for its previous-month query;
you are giving the main query the same control.

### 3.4 Three files declare the same members query privately

`AccountModal.tsx:95-102`, `TransactionsPage.tsx:50-59`, `MembersPanel.tsx:31-39`
— byte-identical `fetchHouseholdMembers` + `useHouseholdMembers` pairs against
the same `["household", "members"]` key. They share a cache entry by coincidence
rather than by construction. Your checklist would be the fourth. Plan Task 1
extracts it first, into `features/settings/` because money already imports
`membersListSchema` from `../settings/schemas`.

**One more, smaller:** `features/money/NetWorthCard.tsx` already exists, already
takes the `Summary` the accounts endpoint returns, and already handles the
not-computable case. The spec listed a new `overview/NetWorthCard.tsx`; the plan
reuses the real one. Do not build a second.

---

## 4 · What M1 learned that applies directly to you

`docs/LEARNING.md` has the full entries. These four will bite M2 specifically.

**A test that pins copy verbatim breaks when you change the copy — and you will
not be told how many.** M1's plan predicted one existing test would break when a
button was renamed. Six did. Another task's plan omitted a test file from its
`git add` list. Before changing any user-visible string, grep the test tree for
it. Adapt by retargeting the handle, never by dropping an assertion.

**A regex query can match two elements once you add a heading.** M1 added an
empty-state title "Add an account first" and an existing test's
`getByText(/add an account first/i)` began matching both it and the header hint
"Add an account first, and transactions can attach to it.", which throws. If you
add a string that is a prefix of one already on screen, narrow the existing
query in the same commit.

**Green tests are not evidence about layout.** M1's layout defect passed every
unit test before the fix and would pass every unit test after a regression. It
was found and verified by `getBoundingClientRect()` in a real browser. A test
asserting a Tailwind class pins the implementation, not the behaviour — the
product owner and I agreed M1's container ships without one, on that reasoning.
Expect the same judgement to apply to your card grid.

**A grep for a concept has to enumerate its phrasings.** M1's audience-copy task
searched `two owners`, `two of you`, `both of you` and reported clean. The
browser walk then found a live `"Sign in to pick up where you both left off."`
None of the three patterns match `you both`. Whatever you sweep for, list the
ways it can be said, and let the walk be the check on your list.

---

## 5 · Running it, and the browser-walk mechanics

```bash
make dev     # everything, logs tailed — http://localhost:5173
make seed    # the design's household; prints sign-in details and an invite URL
make lint    # arch lint, frontend typecheck, eslint, go vet
make test-web
```

**`make test` does run here**, contrary to what M1's plan assumed. It needs the
colima socket exported first:

```bash
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make test
```

It takes about two and a half minutes; `adapter/postgres` alone is 78s of real
testcontainers.

Seeded credentials, printed by `make seed`:
`andreas@hearth.family` / `hearth-dev-password`. Mail lands at
http://localhost:8025 — Mailpit's API is easier than its UI:

```bash
curl -s "http://localhost:8025/api/v1/messages?limit=1"
curl -s "http://localhost:8025/api/v1/message/<ID>"
```

### Browser automation quirks that cost M1 real time

- **Clicking by element reference goes stale.** References captured in the same
  batch as a `navigate` often refer to a tree React has since re-rendered. Sign-out
  in particular never fired via a reference click; it worked immediately via
  `document.querySelector('button[aria-label="Sign out"]').click()` in page
  script.
- **React controlled inputs ignore synthetic typing.** Set them through the
  native setter and dispatch the event, or the component's state never updates:

  ```js
  const set = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
  set.call(el, 'value'); el.dispatchEvent(new Event('input', { bubbles: true }));
  ```
  Use `'change'` and `HTMLSelectElement` for a `<select>`.
- **Screenshots are scaled and easy to misread.** M1 twice concluded an element
  was missing when it was rendered far to the right at a tiny scale. Measure with
  `getBoundingClientRect()` and assert on numbers; use screenshots as the record,
  not as the evidence.
- **Two byte-identical before/after screenshots mean the change did not land.**
  That exact failure is in `LEARNING.md` from the finance-fixes branch. Compare
  their hashes.

---

## 6 · What "done" means here

From `CLAUDE.md`, and the bar M1 was held to:

`make lint && make test` green, at least one new test **mutation-checked** (break
the code on purpose, watch the test go red, restore, and record the failure you
observed), and `FEATURE_TRACKER.md`, `LEARNING.md` and `SYSTEM_DESIGN.md` updated
as part of the work.

**And a browser walk before any "done" claim.** The product owner asked for this
explicitly on 2026-07-30, after a feature that verified 15 of 15 still surprised
them in first-run use. M1's walk is
`docs/superpowers/plans/2026-07-31-hearth-ux-repair-verification.md` — copy its
shape. One numbered criterion per check, each marked pass or fail, with a note
wherever a criterion was met by an interpreted rather than a literal path. A note
is not a failure; a silent pass over an interpreted path is.

Your walk must cover **three member states**, because the seeded owner never
exercises two of them:

1. a fresh household (checklist at 1 of 4, then 4 of 4 and gone)
2. an established household (no checklist, both cards populated — and the net
   worth figure must equal the one on `/money`; two cards disagreeing about the
   same number is the defect this walk exists to catch)
3. a limited member with money, and one without

**Tracker rows for M2:** `FEATURE_TRACKER.md` §4. Net worth card, budget card and
the "+ Add" menu go ⬜ → 🟡 with the gap named, not ✅ — the designed cards carry
detail the interim ones will not. The setup checklist is a new row; the design
does not draw it. The remaining five cards stay ⬜. Recount the summary table by
the file's own stated rule (the first symbol in each row's cell) and confirm the
columns sum; it was 45 / 7 / 41 / 2 = 95 after M1.

---

## 7 · Open items you are inheriting

None of these blocks M2. They are listed so you do not rediscover them and think
they are new.

**The not-found page is a dead end, and that is a product decision, not a bug.**
M1 deleted the Marriage and Family routes, so their URLs now reach
`notFoundComponent` — which sits *above* the authenticated shell. Two
consequences, both documented in `router.tsx`'s header comment and pinned by
`router.test.tsx`:

- the 404 renders with no sidebar and no link home
- `RequireAuth` never mounts, so a signed-out visitor with an old bookmark gets
  bare "Page not found." text instead of the sign-in screen

Only URLs that never shipped to a customer are affected, and the server refuses
regardless. The smallest fix is a `<Link to="/">` inside `notFoundComponent`,
which helps every 404 rather than these two; making the signed-out case redirect
needs the not-found route moved under the shell, which is structural. **Ask
before doing either — it is the owner's call and it is not in M2's scope.**

**Three cosmetic items parked at M1's final review**, any of which you may pick
up if you are already in the file:

- `docs/SYSTEM_DESIGN.md:739` — a parenthetical whose antecedent is ambiguous.
  Not wrong, only loose.
- `SignUpCompleteScreen.tsx:43,47` — two action labels still read "Create a
  household" for a click that navigates to the email form. Defensible as a
  call-to-action; a fourth phrasing that escaped the same sweep that missed "you
  both".
- `LEARNING.md` could use an entry for "a comment that records a false
  justification" — M1's final review found a tombstone comment claiming a deleted
  test's coverage lived elsewhere when it did not, leaving a branch covered by
  nothing. That is a distinct failure from the four already written up.

**Housekeeping, needs the owner's consent because it deletes a directory:**
`.claude/worktrees/transactions/` is a registered git worktree still holding the
pre-M1 two-person strings. It does not affect lint or tests, but it is a live
decoy for exactly the repo-root grep sweep `LEARNING.md` now warns about — anyone
re-sweeping the audience copy gets eight false positives from it.
`git worktree remove .claude/worktrees/transactions` when they agree.

---

## 8 · How to start

1. Read `docs/superpowers/plans/2026-07-31-hearth-interim-overview.md` end to
   end. It has the code, the tests, the run commands and the mutation for each
   task.
2. Read §3 of this document again. Those four facts are the ones the plan
   depends on and the ones you would otherwise assume wrong.
3. Branch from `fix/ux-repair` (or `main` after #4 merges). Do not work on
   `main`.
4. Tasks 1 and 2 are extractions with no behaviour change — their existing test
   suites must pass untouched. If one moves, the extraction changed something;
   find out what rather than adjusting the test.
5. Task 3 is the first task that ships anything a user sees. After it, `/` is a
   real page and you can walk it.

The plan's own self-review section lists what it covers against the spec and the
two places it deliberately departs from it. Read that before you decide the plan
is wrong about something — it may already have explained itself.
