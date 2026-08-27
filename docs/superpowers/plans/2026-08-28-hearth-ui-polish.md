# Hearth UI Polish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give Hearth the interaction states it has never had, fix seven reproduced defects, and settle three places the app contradicts itself.

**Architecture:** Three milestones, three pull requests, in order. M1 is almost entirely `web/src/index.css` — the repo's designated single source of design tokens — plus the shared class constants that every page already composes from; it lands first so every later browser walk happens against the final skin. M2 is seven independent defects, one of which (the Transactions month contract) is a Go handler change. M3 is three small consistency fixes in the frontend.

**Tech Stack:** Go 1.24.2 (clean architecture, `internal/domain` → `internal/usecase` → `internal/adapter/**`), React 19 + TypeScript, TanStack Router + Query, Tailwind CSS v4 (`@theme` tokens in `index.css`, no config file), Vitest + Testing Library, Go stdlib testing with testcontainers.

**Spec:** `docs/superpowers/specs/2026-08-28-hearth-ui-polish-design.md`

## Global Constraints

Copied from CLAUDE.md and the spec. Every task's requirements implicitly include these.

- **Nothing hard-codes a hex value.** Colours, radii, shadows and durations come from `@theme` tokens in `web/src/index.css`.
- **Money is `int64` minor units plus an ISO 4217 code.** `float64` never appears in a monetary path.
- **Clean architecture, enforced by `make lint-arch` including test files.** `internal/domain` imports the standard library only; `internal/usecase` may add `internal/domain`; everything else lives under `internal/adapter/**` or `cmd/**`. No database or HTTP type crosses out of the adapter layer.
- **Authorisation exists only in the HTTP layer.** No service takes an actor parameter.
- **Every 2xx except 204 carries a JSON body**, because the frontend's `apiFetch` throws on an ok response it cannot parse.
- **Fail closed on values you did not construct.** A `switch` over a type arriving from a database column or a request needs a `default` that refuses.
- **Comments say why, never what.** Exported things carry their contract in a doc comment; `usecase/ports.go` is the model.
- **Definition of done:** `make lint && make test` green, at least one new test mutation-checked, `docs/FEATURE_TRACKER.md` and `docs/LEARNING.md` updated. Full checklist at the end of `docs/LEARNING.md`.
- **Browser-verify before claiming done.** Drive the running app at http://localhost:5173 — this is an explicit product-owner requirement, not a suggestion.

### About the test snippets in this plan

Every test below is written against the conventions of the file it joins, but **the render-helper names are not verified**. `renderAppShell`, `renderSidebar`, `renderTransactionModal` and `renderTransactionsPage` are written as the names those files *probably* use. Open the neighbouring tests first and use whatever helper is actually there — several of these files use `renderWithRouter` from `web/src/test/renderWithRouter.tsx` directly, and `GoalsCard.test.tsx` is confirmed to.

Two conventions that are verified and do matter:

- **The first query in a test is `find`, not `get`.** `renderWithRouter` mounts a real TanStack `RouterProvider` whose initial transition resolves asynchronously even with no fetch in play; a synchronous first assertion finds nothing. `GoalsCard.test.tsx` carries the comment explaining this.
- **jsdom does not implement `<dialog>`.** No `showModal()`, no `close()`, no `cancel` event. `Modal.tsx` feature-detects around this. Any test touching modal focus proves the fallback path only — which is why Task 12 has a browser step that is not optional.

### Environment

`go` is not on `PATH` in a bare shell on this machine. Before any `go` command or any `make` target that shells out to one:

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
```

Frontend-only tasks can run `npx vitest` from `web/` without any of the above.

### Branch and PR shape

Branch `ui-polish` is checked out with the spec committed. Each milestone is its own PR off `main`:

- M1 → branch `ui-polish` (already current) → PR "M1 — the interaction skin"
- M2 → branch `ui-defects` off `main` after M1 merges
- M3 → branch `ui-consistency` off `main` after M2 merges

---

# M1 — The interaction skin

## Task 1: Focus ring, transition token, reduced motion

**Files:**
- Modify: `web/src/index.css:1-60`
- Test: `web/src/index.css` has no test; verification is the browser walk in Task 5

**Interfaces:**
- Consumes: nothing
- Produces: CSS custom properties `--color-focus`, `--focus-ring-width`, `--focus-ring-offset`, `--transition-state`; a global `:focus-visible` rule; a `prefers-reduced-motion` block. Tasks 2-4 reference `--transition-state` by name.

Chrome's default focus ring was measured live on this app at `rgb(0, 95, 204)` / `0.8px`. Nothing in the codebase sets `outline-none`, so this rule **replaces** that ring everywhere at once rather than adding one element by element.

- [ ] **Step 1: Add the tokens to `@theme`**

In `web/src/index.css`, inside the existing `@theme` block, after the `--shadow-auth-card` line:

```css
  /* The focus ring. Chrome's default is rgb(0, 95, 204) at 0.8px -- an
     off-palette blue, and too thin to see against --color-canvas. Nothing in
     this codebase sets outline-none, so the :focus-visible rule below
     substitutes for that default on every focusable element at once rather
     than being applied per component. Built on the accent rather than a new
     colour: a focus ring is the app pointing at itself, and the app is green. */
  --color-focus: var(--color-accent);
  --focus-ring-width: 2px;
  --focus-ring-offset: 2px;

  /* One duration for every hover/active/focus change, so two components can
     never disagree about how fast the interface responds. 160ms is under the
     ~200ms at which a state change stops reading as instant and starts
     reading as animation -- these are feedback, not motion. */
  --transition-state: 160ms;
```

- [ ] **Step 2: Add the global focus-visible rule and the reduced-motion guard**

At the end of `web/src/index.css`, after the existing `body` rule:

```css
/* :focus-visible, not :focus -- a mouse click on a button should not leave a
   ring the pointer user never asked for, which :focus would. Keyboard focus,
   and focus moved programmatically (Modal.tsx does this on open), still ring. */
:focus-visible {
  outline: var(--focus-ring-width) solid var(--color-focus);
  outline-offset: var(--focus-ring-offset);
}

/* Tabular figures. NOT on body: a heading containing a year should stay
   proportional. Applied at the elements that render formatMoney's output --
   see formatMoney.ts, which already spends a comment on using U+2212 MINUS
   rather than a hyphen "because every negative figure in this app is in a
   column of numbers". This is the same argument, made about the digits. */
.tabular {
  font-variant-numeric: tabular-nums;
}

/* Honours the OS setting for every state change the tokens above drive.
   Added with the transitions rather than after them, because a transition
   shipped without this is one somebody has to remember to come back for. */
@media (prefers-reduced-motion: reduce) {
  :root {
    --transition-state: 0ms;
  }
}
```

- [ ] **Step 3: Verify the app still builds and types check**

```bash
cd web && npx tsc --noEmit && npx vite build
```

Expected: both succeed. (`index.css` has no types, but a malformed `@theme` block fails the Tailwind v4 build.)

- [ ] **Step 4: Verify the ring renders in a real browser**

Start the app if it is not running (`make dev`), open http://localhost:5173, press Tab, and confirm the first focused element carries a green ring rather than a blue one. Then in the console:

```js
getComputedStyle(document.activeElement).outlineColor
```

Expected: an `rgb(26, 107, 82)`-family value (the accent), **not** `rgb(0, 95, 204)`.

- [ ] **Step 5: Commit**

```bash
git add web/src/index.css
git commit -m "feat(ui): give focus a ring the palette owns

Chrome's default ring, measured on this app at rgb(0, 95, 204) / 0.8px, was
the app's entire focus treatment: focus-visible and focus appear zero times
across ~90 components. Nothing sets outline-none, so one rule substitutes for
it everywhere.

Adds --transition-state and its prefers-reduced-motion guard in the same
change, so the transitions Tasks 2-4 add are never shipped without it."
```

---

## Task 2: Tabular figures on the money columns

**Files:**
- Modify: `web/src/features/money/TransactionsPage.tsx:221` area (the amount span)
- Modify: `web/src/features/money/BreakdownCard.tsx:44,55`
- Modify: `web/src/features/money/BudgetStatCards.tsx`
- Modify: `web/src/features/money/BillStatCards.tsx`
- Modify: `web/src/features/overview/BudgetCard.tsx`
- Modify: `web/src/features/money/NetWorthCard.tsx`
- Test: none. This is a class-application sweep; the only unit test it could carry would assert a Tailwind class string, which the review rubric treats as a test that asserts nothing. Verification is the font probe in Step 1 and the column check in Step 5, with the existing suite as a regression net.

**`BreakdownCard.tsx`'s Net row (line 55) is Task 16's, not this task's.** Add `.tabular` only to the per-type row span at line 44 here; Task 16 rewrites the Net span entirely and carries the class itself.

**Interfaces:**
- Consumes: the `.tabular` class from Task 1
- Produces: no new exports. Later tasks do not depend on this.

**Before spreading the class, confirm the font actually has the feature.** Schibsted Grotesk ships tabular figures, but a face without them ignores `font-variant-numeric` silently, which would make this task look done while changing nothing.

- [ ] **Step 1: Confirm the font supports tabular figures, in a real browser**

With the app running, in the console:

```js
const mk = (v) => { const s = document.createElement('span');
  s.style.font = getComputedStyle(document.body).font;
  s.style.fontVariantNumeric = v; s.textContent = '1111'; s.style.position='fixed';
  document.body.appendChild(s); const w = s.getBoundingClientRect().width;
  s.textContent = '0000'; const w2 = s.getBoundingClientRect().width;
  s.remove(); return [w, w2]; };
console.log('normal', mk('normal'), 'tabular', mk('tabular-nums'));
```

Expected: with `tabular-nums`, the two widths are equal. With `normal` they may differ. If they are equal in **both** cases the font has no proportional figures and this task is a no-op — stop and report that rather than adding a class that does nothing.

- [ ] **Step 2: Apply the class at the ledger's amount column**

In `web/src/features/money/TransactionsPage.tsx`, the amount span rendered beside each row's description. Add `tabular` to its existing `className`, e.g.:

```tsx
<div className="tabular text-[13.5px] font-semibold text-ink">
```

- [ ] **Step 3: Apply it at the remaining money render sites**

The same one-word addition in each of: `BreakdownCard.tsx` (the per-type row's amount span at line 44 **only** — see the note above), `BudgetStatCards.tsx`, `BillStatCards.tsx`, `overview/BudgetCard.tsx`, `NetWorthCard.tsx`.

Only spans that render `formatMoney(...)` output. Not headings, not dates, not counts.

- [ ] **Step 4: Run the frontend suite**

```bash
cd web && npx vitest run
```

Expected: PASS. No test asserts on these class strings, so this is a regression check, not a proof.

- [ ] **Step 5: Verify column alignment in a real browser**

Open http://localhost:5173/money/transactions signed in as a household with several transactions (the seeded household, `andreas@hearth.family` / `hearth-dev-password`, has a populated ledger). Confirm the decimal points down the amount column line up.

- [ ] **Step 6: Commit**

```bash
git add web/src/features
git commit -m "feat(ui): tabular figures in the money columns

formatMoney already spends a comment on U+2212 MINUS 'because every negative
figure in this app is in a column of numbers'. The digits were still
proportional, so the columns did not line up. Same argument, applied to the
digits."
```

---

## Task 3: Hover and active on the shared surfaces

**Files:**
- Modify: `web/src/features/shell/Sidebar.tsx:73-75` (`NAV_ITEM_CLASS`)
- Modify: `web/src/features/money/TransactionsPage.tsx` (the row container)
- Modify: `web/src/features/money/BillRow.tsx`
- Modify: `web/src/features/marriage/RetroHistoryList.tsx`
- Test: `web/src/features/shell/Sidebar.test.tsx`

**Interfaces:**
- Consumes: `--transition-state` from Task 1
- Produces: no new exports.

Only four `hover:` rules exist in the whole app today, all in `BudgetPage.tsx` and `BudgetHistoryModal.tsx`. This task adds them where they carry the most weight: the nav, and the list rows that are already clickable.

**Read `Sidebar.tsx`'s comment above `NAV_ITEM_CLASS` before editing it.** It documents a shipped defect: a link carrying both `text-ink` and a conditional `text-accent` renders ink regardless of order, because Tailwind's cascade picks whichever generated rule sits later in the stylesheet, not whichever appears later in the class string. Each link must carry exactly one colour class. **Do not add a `hover:text-*` that fights the active colour** — use a background instead.

- [ ] **Step 1: Add hover and active to `NAV_ITEM_CLASS`**

```tsx
const NAV_ITEM_CLASS =
  "inline-flex min-h-11 items-center rounded-lg px-2.5 py-2 text-[13.5px] font-semibold " +
  // Background, never a hover:text-* -- the comment above explains why a
  // second colour class on these links cannot be relied on to win. A
  // background is a different property and does not enter that fight.
  "transition-colors duration-[var(--transition-state)] hover:bg-canvas active:bg-toggle-off " +
  "lg:min-h-[auto]";
```

- [ ] **Step 2: Run the Sidebar tests to confirm nothing depended on the exact class string**

```bash
cd web && npx vitest run src/features/shell/Sidebar.test.tsx
```

Expected: PASS. If a test asserts on the literal class string, update the assertion to check the behaviour (the active link's colour) rather than the string.

- [ ] **Step 3: Add hover to the clickable list rows**

In `TransactionsPage.tsx`'s row container, `BillRow.tsx`'s row, and `RetroHistoryList.tsx`'s items, add to each existing `className`:

```
transition-colors duration-[var(--transition-state)] hover:bg-canvas
```

Rows that are not clickable get nothing — a hover state on an inert row is a lie about what will happen.

- [ ] **Step 4: Run the full frontend suite**

```bash
cd web && npx vitest run
```

Expected: PASS.

- [ ] **Step 5: Verify in a real browser**

Hover each nav link and confirm the background shifts and the active link keeps its accent colour. Press and hold a nav link and confirm the pressed background differs from hover. Hover a transaction row, a bill row and a retro row.

- [ ] **Step 6: Commit**

```bash
git add web/src/features
git commit -m "feat(ui): hover and active on the nav and the clickable rows

Four hover rules existed across ~90 components, all in Budget. Nav links and
list rows are where the cursor spends its time.

Backgrounds rather than hover:text-*, because NAV_ITEM_CLASS's own comment
records a shipped defect where a second colour class on these links lost to
the cascade regardless of class-string order."
```

---

## Task 4: The skip link

**Files:**
- Modify: `web/src/features/shell/AppShell.tsx`
- Test: `web/src/features/shell/AppShell.test.tsx`

**Interfaces:**
- Consumes: `--color-focus` from Task 1
- Produces: `<main id="main-content">` — nothing else references this id.

Measured on the running app: the first tabbable element on every page is the Overview nav link. A keyboard user passes eight nav links before content, on every navigation.

- [ ] **Step 1: Write the failing test**

Add to `web/src/features/shell/AppShell.test.tsx`:

```tsx
it("puts a skip link ahead of the navigation, pointing at the page content", async () => {
  renderAppShell();

  const skip = await screen.findByRole("link", { name: /skip to content/i });

  // Ahead of the nav in DOM order, which is what makes it the first thing a
  // keyboard reaches -- a skip link rendered after the nav it skips is
  // decoration.
  const nav = screen.getByRole("navigation");
  expect(skip.compareDocumentPosition(nav) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

  expect(skip).toHaveAttribute("href", "#main-content");
  expect(document.querySelector("#main-content")?.tagName).toBe("MAIN");
});
```

Match the existing file's render helper name; if `renderAppShell` does not exist there, use whatever helper the neighbouring tests use.

- [ ] **Step 2: Run it to verify it fails**

```bash
cd web && npx vitest run src/features/shell/AppShell.test.tsx
```

Expected: FAIL — "Unable to find an accessible element with the role 'link' and name /skip to content/i".

- [ ] **Step 3: Add the skip link and the target id**

In `AppShell.tsx`, immediately inside the outer `<div>` and **before** `<MobileTopBar>`:

```tsx
{/* First in DOM order, so it is the first thing Tab reaches. Visually hidden
    until focused: sr-only alone would keep it hidden even while focused,
    which makes it unusable for the sighted keyboard user it exists for. */}
<a
  href="#main-content"
  className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-[60] focus:rounded-lg focus:bg-card focus:px-4 focus:py-2 focus:text-[13px] focus:font-semibold focus:text-ink"
>
  Skip to content
</a>
```

Then give `<main>` the id:

```tsx
<main id="main-content" inert={navOpen || undefined} className="lg:overflow-y-auto">
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd web && npx vitest run src/features/shell/AppShell.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Mutation-check the test**

Move the `<a>` to *after* `<NavDrawer>` in `AppShell.tsx` and re-run. Expected: FAIL on the `compareDocumentPosition` assertion. Restore the correct position and confirm PASS again.

This is the assertion that matters — a skip link that renders after the nav satisfies a naive "does it exist" test while helping nobody.

- [ ] **Step 6: Verify in a real browser**

Load any page, press Tab once. The skip link must become visible. Press Enter and confirm focus lands in the page content.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/shell
git commit -m "feat(a11y): skip link ahead of the navigation

Measured on the running app: the first tabbable element on every page was the
Overview nav link, so a keyboard user passed eight links before content on
every navigation.

The test asserts DOM order, not merely presence -- a skip link rendered after
the nav it skips passes a presence check and helps nobody."
```

---

## Task 5: Drop the three unused font families

**Files:**
- Modify: `web/index.html:8-13` (the Google Fonts `<link>`)
- Modify: `web/src/index.css:46-47` (`--font-alt`, `--font-mono`)

**Interfaces:**
- Consumes: nothing
- Produces: nothing. This removes two tokens that zero components reference.

Counted: `font-alt` appears in 0 components, `font-mono` in 0, and IBM Plex Sans is loaded with no token at all. Three families are downloaded on every cold load for nothing.

- [ ] **Step 1: Confirm the count before deleting**

```bash
cd web/src && for t in font-sans font-serif font-alt font-mono; do \
  printf "%-12s %s\n" "$t" "$(grep -rl "$t" features components 2>/dev/null | grep -v '\.test\.' | wc -l | tr -d ' ')"; done
grep -rn "Karla\|Plex" . | grep -v node_modules
```

Expected: `font-alt 0`, `font-mono 0`, and the only `Karla`/`Plex` hits are the two `index.css` token lines. **If any component uses either token, stop** — the task's premise is wrong and it should be reported rather than forced.

- [ ] **Step 2: Trim the font request**

In `web/index.html`, reduce the Google Fonts `href` to the two families actually in use, keeping the existing weights and `display=swap`:

```html
<link
  href="https://fonts.googleapis.com/css2?family=Schibsted+Grotesk:wght@400;500;600;700&amp;family=Newsreader:ital,opsz,wght@0,6..72,400;0,6..72,500;0,6..72,600;1,6..72,400&amp;display=swap"
  rel="stylesheet"
/>
```

- [ ] **Step 3: Remove the two dead tokens**

Delete the `--font-alt` and `--font-mono` lines from `web/src/index.css`.

Leave a note where they were, because the next person will wonder:

```css
  /* --font-alt (Karla) and --font-mono (IBM Plex Mono) were defined here and
     referenced by nothing, alongside an IBM Plex Sans that had no token at
     all -- three families downloaded on every cold load for no glyph. A
     monospace face for figures is a decision worth taking deliberately, with
     tabular figures already in hand; it should not be taken by picking up a
     token that happened to be lying here. */
```

- [ ] **Step 4: Build and run the suite**

```bash
cd web && npx tsc --noEmit && npx vite build && npx vitest run
```

Expected: all PASS.

- [ ] **Step 5: Verify no text changed appearance**

Load the app and confirm headings still render in Newsreader (the serif) and body text in Schibsted Grotesk. Check Overview, Finances and a modal.

- [ ] **Step 6: Commit**

```bash
git add web/index.html web/src/index.css
git commit -m "chore(ui): stop downloading three unused font families

Karla and IBM Plex Mono were tokens no component referenced; IBM Plex Sans was
loaded with no token at all. Three families on every cold load, zero glyphs
rendered."
```

---

## Task 6: M1 browser walk and docs

**Files:**
- Modify: `docs/LEARNING.md`

**Interfaces:**
- Consumes: everything in M1
- Produces: nothing

- [ ] **Step 1: Walk every page at desktop width**

With `make dev` running, signed into the seeded household, open each of: `/`, `/money`, `/money/transactions`, `/money/budget`, `/money/goals`, `/money/bills`, `/marriage/retros`, `/settings`.

On each: Tab through and confirm the green ring; hover the nav and any list rows; confirm no layout shifted.

- [ ] **Step 2: Walk the same pages at 390px**

Use CDP viewport emulation (chrome-devtools MCP `resize_page` to 390×844) rather than resizing the OS window — a maximised or fullscreen Chrome window ignores `resize_window`, which is why the original audit's first attempt read 2752px back after asking for 1440px.

Confirm the drawer still opens, the skip link still appears on first Tab, and no hover state is stuck on after a tap.

- [ ] **Step 3: Confirm reduced motion is honoured**

In the browser's rendering panel, force `prefers-reduced-motion: reduce`, then hover a nav link. The background must change instantly rather than easing.

- [ ] **Step 4: Add the M1 entry to `docs/LEARNING.md`**

One entry. What broke: the app had zero focus, active and tabular-nums rules and four hover rules across ~90 components, so its focus treatment was Chrome's default blue inside a cream-and-forest palette. What the symptom looked like: nothing — no test could fail, because no test asserts on interaction states. What would have caught it sooner: a periodic count of interaction-state utilities, or a browser walk done with the keyboard rather than the mouse.

- [ ] **Step 5: Run the full gate**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

Expected: green.

- [ ] **Step 6: Commit and open the PR**

```bash
git add docs/LEARNING.md
git commit -m "docs: what M1 taught"
git push -u origin ui-polish
gh pr create --title "M1 — the interaction skin" --body "$(cat <<'EOF'
Focus ring, tabular figures, hover and active, reduced motion, a skip link,
and three unused font families removed.

Counted before: focus-visible 0, active 0, tabular-nums 0, hover 4, across
~90 components. Chrome's default ring, measured on this app, was
rgb(0, 95, 204) at 0.8px.

Walked at desktop and 390px. Spec:
docs/superpowers/specs/2026-08-28-hearth-ui-polish-design.md

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01RbrEfoWQwDXmqQcbBp916b
EOF
)"
```

---

# M2 — The seven defects

Branch off `main` once M1 has merged:

```bash
git checkout main && git pull && git checkout -b ui-defects
```

## Task 7: The Transactions month contract

**Files:**
- Modify: `api/internal/adapter/http/transaction_handlers.go:227-238` (`parseTransactionFilter`)
- Modify: `api/internal/usecase/ports.go:494-506` (`TransactionFilter.Month` doc comment)
- Modify: `web/src/features/money/useTransactions.ts:44`
- Test: `api/internal/adapter/http/transactions_api_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: the wire contract `month=all` meaning every month; the default (no `month` parameter) meaning the current month, applied to **both** the list and the summary.

The bug, exactly: `parseTransactionFilter` sets `month := time.Now().UTC()` unconditionally, but sets `filter.Month` only inside the `if raw := q.Get("month"); raw != ""` branch. So the default request lists every month while summarising the current one. `handleListTransactions`'s own doc comment says the two "must describe the same month".

- [ ] **Step 1: Write the failing test**

Add to `api/internal/adapter/http/transactions_api_test.go`. Follow the file's existing `newTestEnv(t)` / `env.signIn(t, ...)` / `env.authed(...)` conventions — read `TestTransactionWriteRoutesRequireCSRF` first for the exact helper signatures.

```go
// TestListTransactionsDefaultsListAndSummaryToTheSameMonth drives the ledger
// with no month filter at all -- the state the screen opens in.
//
// handleListTransactions's own doc comment states the contract: it "serves the
// ledger and the two figures above it together, because they are one screen and
// must describe the same month." parseTransactionFilter broke it by defaulting
// the summary's month unconditionally while leaving filter.Month zero, which
// TransactionFilter documents as "every month". The screen therefore read
// "0 in August 2026" over a list of July rows.
//
// The assertion is on the transactions' own dates against summary.month, not on
// the count alone: a count check would stay green if both halves were wrong in
// the same direction.
func TestListTransactionsDefaultsListAndSummaryToTheSameMonth(t *testing.T) {
	env := newTestEnv(t)
	session, csrf := env.signIn(t, env.ownerEmail, env.ownerPassword)

	accountID := env.createAccount(t, session, csrf, "UOB", "SGD", 100000)

	now := time.Now().UTC()
	lastMonth := now.AddDate(0, -1, 0)

	env.createTransaction(t, session, csrf, accountID, "This month", 2000, now)
	env.createTransaction(t, session, csrf, accountID, "Last month", 3000, lastMonth)

	var body struct {
		Transactions []struct {
			OccurredOn string `json:"occurredOn"`
		} `json:"transactions"`
		Summary struct {
			Month string `json:"month"`
			Count int    `json:"count"`
		} `json:"summary"`
	}
	env.getJSON(t, session, "/api/v1/transactions", &body)

	if body.Summary.Month != now.Format("2006-01") {
		t.Fatalf("summary.month = %q, want the current month %q",
			body.Summary.Month, now.Format("2006-01"))
	}
	if body.Summary.Count != len(body.Transactions) {
		t.Fatalf("summary.count = %d but the list carries %d rows; the two halves of one screen disagree",
			body.Summary.Count, len(body.Transactions))
	}
	for _, txn := range body.Transactions {
		if !strings.HasPrefix(txn.OccurredOn, body.Summary.Month) {
			t.Errorf("listed a transaction on %s while the summary describes %s",
				txn.OccurredOn, body.Summary.Month)
		}
	}
}
```

If `env.createAccount` / `env.createTransaction` / `env.getJSON` do not exist with those signatures, use whatever the file's neighbouring tests use and adjust; do not invent helpers that are not there.

- [ ] **Step 2: Run it to verify it fails**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
cd api && go test ./internal/adapter/http/ -run TestListTransactionsDefaultsListAndSummaryToTheSameMonth -v
```

Expected: FAIL — the list carries the last-month row while the summary describes this month.

- [ ] **Step 3: Fix `parseTransactionFilter`**

In `api/internal/adapter/http/transaction_handlers.go`, replace the month block:

```go
	// The default applies to BOTH halves. Setting only `month` here and
	// leaving filter.Month zero -- which TransactionFilter documents as "every
	// month" -- is what let the ledger list July under a header reading
	// "0 in August": handleListTransactions's own contract is that the list
	// and the two figures above it describe the same month.
	//
	// "all" is the one way to widen the list, and it widens the summary with
	// it. Spelled explicitly rather than inferred from an empty value, because
	// an empty `month=` in a query string is what a cleared form control sends
	// by accident, and that must not silently mean something different from
	// the default.
	month := time.Now().UTC()
	if raw := q.Get("month"); raw != "" {
		if raw == monthAll {
			return filter, time.Time{}, true
		}
		parsed, err := time.Parse(monthLayout, raw)
		if err != nil {
			WriteError(w, http.StatusUnprocessableEntity, "INVALID_MONTH",
				"That month could not be read. Use YYYY-MM.", nil)
			return usecase.TransactionFilter{}, time.Time{}, false
		}
		month = parsed
	}
	filter.Month = month
```

Add the constant beside `monthLayout`:

```go
// monthAll is the wire value that widens both the ledger and its summary to
// every month. See parseTransactionFilter.
const monthAll = "all"
```

**Check what a zero `month` does to `MonthSummary`** before assuming the `monthAll` early return is safe — if `MonthSummary` cannot answer "all time", either give it that case or have `monthAll` keep the summary on the current month and label it. Read `api/internal/usecase/transaction.go`'s `MonthSummary` and decide there; do not guess.

- [ ] **Step 4: Update `TransactionFilter.Month`'s doc comment**

In `api/internal/usecase/ports.go`, the comment currently reads "Zero means every month." That is still true of the type, but the HTTP layer no longer sends zero by default. Extend it:

```go
	// Month is any instant inside the calendar month to list. Zero means every
	// month -- but note the HTTP adapter never sends zero by default: an
	// absent `month` parameter defaults to the current month, and only an
	// explicit `month=all` produces zero here (see parseTransactionFilter).
	Month time.Time
```

- [ ] **Step 5: Run the test to verify it passes**

```bash
cd api && go test ./internal/adapter/http/ -run TestListTransactions -v
```

Expected: PASS.

- [ ] **Step 6: Mutation-check the test**

Move `filter.Month = month` back inside the `if` block. Re-run. Expected: FAIL. Restore and confirm PASS.

- [ ] **Step 7: Send the effective month from the frontend**

In `web/src/features/money/useTransactions.ts:44`, the month parameter is currently omitted when empty. Send `all` when the user has deliberately cleared it, so the cleared state is explicit on the wire rather than being read as the default:

```ts
  if (filters.month) params.set("month", filters.month);
  else if (filters.monthCleared) params.set("month", "all");
```

Adjust to the actual filter-state shape in that file; the point is that "not yet chosen" and "deliberately cleared" must reach the server as different values.

- [ ] **Step 8: Run the full Go and frontend suites**

```bash
cd api && go test ./... && cd ../web && npx vitest run
```

Expected: both PASS.

- [ ] **Step 9: Verify in a real browser**

Open `/money/transactions` on the seeded household. The header count must equal the number of rows shown. Change the month filter and confirm both move together. Clear it and confirm the list widens and the header stops naming a month.

- [ ] **Step 10: Update `docs/SYSTEM_DESIGN.md`**

This changes a documented route behaviour. Use the `maintaining-system-design` skill. Update the request-flow prose for `GET /api/v1/transactions` to say the month defaults to the current month for both halves, and that `month=all` widens both.

- [ ] **Step 11: Commit**

```bash
git add api web/src/features/money/useTransactions.ts docs/SYSTEM_DESIGN.md
git commit -m "fix(transactions): make the header describe the list beneath it

parseTransactionFilter defaulted the summary's month unconditionally but set
filter.Month only when ?month= was present, which TransactionFilter documents
as 'every month'. The screen read '0 in August 2026' over ten July rows.

handleListTransactions's own doc comment already stated the contract this
broke: the ledger and the two figures above it 'are one screen and must
describe the same month'."
```

---

## Task 8: The achieved-goal branch on the Overview

**Files:**
- Modify: `web/src/features/overview/GoalsCard.tsx:81-99`
- Modify: `web/src/features/overview/copy.ts`
- Test: `web/src/features/overview/GoalsCard.test.tsx`

**Interfaces:**
- Consumes: `GoalsResponse` from `features/money/goalSchemas`
- Produces: a new `OVERVIEW_COPY.goalsAllAchieved` entry.

When a household's only goal is achieved: `hasAnyGoals` is true (the array is non-empty), but the backend counts an achieved goal in neither `datedCount` nor `noDateCount`, and it is not `nextGoal`. All three clauses are null, the empty state is skipped, and the card paints its heading over blank space.

`GoalsCard.test.tsx` already has a `goalsFixture(summaryOverrides, goals)` helper that can express exactly this — real goals with all-zero summary counts. Its own comment says so.

- [ ] **Step 1: Write the failing test**

Add to `web/src/features/overview/GoalsCard.test.tsx`, after the existing "no goals at all" test:

```tsx
// The state a household reaches by succeeding: its only goal is funded and
// not yet archived. An achieved goal is counted in neither datedCount nor
// noDateCount (the backend's List loop checks achieved before the dated/
// undated split) and is never nextGoal -- so every clause below hasAnyGoals
// is null at once, and the card rendered its heading over blank space.
it("says something true when every goal is already achieved", async () => {
  renderWithRouter(
    <GoalsCard
      goals={goalsFixture({ datedCount: 0, noDateCount: 0, nextGoal: null }, [
        goalFixture({ id: "g1", name: "Japan 2027", status: "achieved", percent: 100 }),
      ])}
    />,
  );

  expect(await screen.findByText("Goals on track")).toBeInTheDocument();
  expect(screen.getByText("All goals reached")).toBeInTheDocument();

  // Not the never-had-a-goal empty state: this household has one, and it won.
  expect(screen.queryByText("No goals yet")).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd web && npx vitest run src/features/overview/GoalsCard.test.tsx
```

Expected: FAIL — "Unable to find an element with the text: All goals reached".

- [ ] **Step 3: Add the copy**

In `web/src/features/overview/copy.ts`, beside the other `goals*` entries:

```ts
  // Every live goal is achieved. Distinct from goalsNone (never had one) and
  // from the dateless case (has goals, none dated): those two both have
  // something left to do, and this one does not.
  goalsAllAchieved: "All goals reached",
```

- [ ] **Step 4: Add the fourth branch**

In `GoalsCard.tsx`, compute the condition beside the existing `hasAnyGoals`:

```ts
  // Goals exist, but not one of them is dated, undated or next -- which is
  // only reachable when every live goal is achieved, since the backend counts
  // an achieved goal in neither split. Without this the three clauses below
  // are all null and the card renders a heading over nothing.
  const allAchieved = hasAnyGoals && !trackClause && !noDateClause && !summary.nextGoal;
```

Then render it as the first clause inside the `hasAnyGoals` branch:

```tsx
        <>
          {allAchieved && (
            <p className="mt-1.5 text-[30px] font-semibold tracking-[-0.03em] text-ink">
              {OVERVIEW_COPY.goalsAllAchieved}
            </p>
          )}
          {trackClause && (
```

- [ ] **Step 5: Run the test to verify it passes**

```bash
cd web && npx vitest run src/features/overview/GoalsCard.test.tsx
```

Expected: PASS, and the three pre-existing tests still PASS — `allAchieved` must be false whenever any clause is non-null.

- [ ] **Step 6: Mutation-check the test**

Change `allAchieved` to `const allAchieved = false;`. Re-run. Expected: the new test FAILS, the other three still pass. Restore and confirm all four pass.

- [ ] **Step 7: Verify in a real browser**

On a household whose only goal is achieved, load `/`. The "Goals on track" card must read "All goals reached" rather than showing a heading over blank space. (The primary household on this machine is in exactly this state; the seeded one is not.)

- [ ] **Step 8: Commit**

```bash
git add web/src/features/overview
git commit -m "fix(overview): say something when every goal is achieved

hasAnyGoals is true for a household whose only goal is achieved, but that goal
is counted in neither datedCount nor noDateCount and is never nextGoal -- so
all three clauses were null at once and the card rendered its heading over
blank space.

The component's own comments name this shape twice. The guard was right; there
was simply no fourth branch behind it."
```

---

## Task 9: Let the app's own validation messages run

**Files:**
- Modify: `web/src/features/money/TransactionModal.tsx:315` (the `<form>`), and each `required` attribute on it
- Test: `web/src/features/money/TransactionModal.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing new. `amountError` already exists and already renders.

The messages are already written and already wired. `TransactionModal` holds `amountError`, sets it from `handleSubmit` via `describeAmountError`, and renders it at line 372. An empty field never reaches any of it, because `noValidate` appears in **zero** of the app's fifteen forms, so the browser intercepts the submit event first. The file says so in its own comment at lines 236-241.

`toMinorUnits("")` already returns `null`, which already produces "Enter an amount, like 52.30." — so this is one attribute plus per-field guards, not a new error system.

- [ ] **Step 1: Write the failing test**

Add to `web/src/features/money/TransactionModal.test.tsx`, matching the file's existing render helper:

```tsx
// Submitting empty used to be intercepted by the browser's own constraint
// validation, so handleSubmit never ran and the app's own message -- which
// exists, and is written -- was never shown. jsdom implements constraint
// validation, so this test genuinely covers the interception.
it("shows its own message on an empty amount rather than deferring to the browser", async () => {
  const onSubmit = vi.fn();
  renderTransactionModal({ onSubmit });

  await userEvent.click(await screen.findByRole("button", { name: /save transaction/i }));

  expect(await screen.findByText(/Enter an amount, like/)).toBeInTheDocument();
  expect(onSubmit).not.toHaveBeenCalled();
});
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd web && npx vitest run src/features/money/TransactionModal.test.tsx
```

Expected: FAIL — the message is not found, because the submit never reached `handleSubmit`.

- [ ] **Step 3: Add `noValidate` and per-field guards**

In `TransactionModal.tsx` line 315:

```tsx
      {/* noValidate: the browser's own constraint validation fires before
          the submit event, so handleSubmit -- and every message this modal
          already writes -- never ran for an empty field. The bubble it showed
          instead is Chrome's, in Chrome's words, with Chrome's blue ring.
          Every `required` below keeps its attribute for semantics and for
          screen readers; the checks in handleSubmit are what now refuse. */}
      <form noValidate className="flex flex-col gap-4" onSubmit={handleSubmit}>
```

Then walk each of the seven `required` fields on this form and confirm `handleSubmit` refuses an empty value for it with a rendered message. `amountInput` is already covered by `toMinorUnits` returning null. For any field that is not covered, add a check in the same shape as the existing ones, setting an error state that renders through a `role="alert"` paragraph beside its field.

**Do not remove any `required` attribute** — it still carries the semantics for assistive technology.

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd web && npx vitest run src/features/money/TransactionModal.test.tsx
```

Expected: PASS, with every pre-existing test in the file still passing.

- [ ] **Step 5: Mutation-check the test**

Remove `noValidate`. Re-run. Expected: FAIL. Restore and confirm PASS.

- [ ] **Step 6: Verify in a real browser**

Open `/money/transactions`, click "+ Add transaction", click "Save transaction" with everything empty. Expected: Hearth's own red message beside the Amount field, in Hearth's typeface — **not** Chrome's grey bubble. Then submit with only the amount filled and confirm each remaining required field refuses with its own message.

- [ ] **Step 7: Commit**

```bash
git add web/src/features/money
git commit -m "fix(transactions): show Hearth's validation message, not Chrome's

The message already existed and was already wired: amountError is set from
handleSubmit via describeAmountError and rendered. An empty field never
reached it, because noValidate appears in zero of the app's fifteen forms and
the browser intercepts submit first.

TransactionModal's own comment already recorded this -- 'a literally empty
required field never reaches this far at all'. The other fourteen forms share
the shape and are named as a follow-up in the spec's Out of scope."
```

---

## Task 10: Delete the ⌘K chip

**Files:**
- Modify: `web/src/features/shell/Sidebar.tsx` (the brand row, and the header comment that describes the chip)
- Test: `web/src/features/shell/Sidebar.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

`Sidebar.tsx` says it outright: "The ⌘K chip is static — no command palette exists to open." It also renders inside the mobile drawer, on devices with no ⌘ key.

- [ ] **Step 1: Write the failing test**

Add to `web/src/features/shell/Sidebar.test.tsx`:

```tsx
// The chip advertised a command palette that does not exist, on desktop and
// in the mobile drawer alike. A palette is a feature and gets its own slice;
// until then the app should not promise it.
it("does not advertise a command palette", async () => {
  renderSidebar();

  expect(await screen.findByText("Hearth")).toBeInTheDocument();
  expect(screen.queryByText("⌘K")).not.toBeInTheDocument();
});
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd web && npx vitest run src/features/shell/Sidebar.test.tsx
```

Expected: FAIL — the chip is found.

- [ ] **Step 3: Delete the chip**

Remove this element from the brand row:

```tsx
        <div className="ml-auto rounded-md border border-hairline px-1.5 py-0.5 text-[11px] text-muted">
          ⌘K
        </div>
```

Update the comment above the brand row, which currently ends "The ⌘K chip is static -- no command palette exists to open." — replace that sentence with a note that the chip was removed and why, so nobody re-adds it from the design file:

```tsx
      {/* The design's sidebar has no separate top bar; this brand row (logo
          square, "Hearth") is the only header it draws, so AppShell's
          "header" is this. The design also draws a ⌘K chip here. It is
          deliberately not rendered: no command palette exists, and the chip
          also appeared in the mobile drawer, on devices with no ⌘ key. It
          comes back the day a palette does. */}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd web && npx vitest run src/features/shell/Sidebar.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Verify in a real browser**

Confirm the chip is gone from the desktop sidebar and from the mobile drawer at 390px, and that the brand row's layout has not collapsed now that `ml-auto` has nothing to push.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/shell
git commit -m "fix(shell): stop advertising a command palette that does not exist

Sidebar.tsx said so itself: 'The ⌘K chip is static -- no command palette
exists to open.' It also rendered in the mobile drawer, on devices with no ⌘
key.

A palette is a feature and gets its own slice. The comment now records why the
chip is absent, so it is not re-added from the design file."
```

---

## Task 11: Show the month the filter is actually on

**Files:**
- Modify: `web/src/features/money/TransactionsPage.tsx` (the filter state's initial value)
- Modify: `web/src/features/money/TransactionFilters.tsx:196-208`
- Test: `web/src/features/money/TransactionsPage.test.tsx`

**Interfaces:**
- Consumes: Task 7's `summary.month` and the `month=all` wire value
- Produces: nothing new

Falls out of Task 7. The Month input opens blank, so Chrome renders its empty-state placeholder — a row of dashes that reads as a broken control. (The original audit called this an `<input type="date">`; it is not, and never was. `TransactionFilters.tsx:202` is `type="month"`.)

- [ ] **Step 1: Write the failing test**

```tsx
// The filter must show the month the list is actually scoped to. Opening
// blank made Chrome draw its empty-month placeholder -- a row of dashes that
// reads as a broken control rather than as "any month".
it("opens with the month the summary describes, not blank", async () => {
  renderTransactionsPage();

  const month = await screen.findByLabelText(/month/i);
  expect(month).toHaveValue("2026-08");
});
```

Set the fetch stub's `summary.month` to `"2026-08"` using the file's existing stub helper.

- [ ] **Step 2: Run it to verify it fails**

```bash
cd web && npx vitest run src/features/money/TransactionsPage.test.tsx
```

Expected: FAIL — the value is `""`.

- [ ] **Step 3: Seed the filter from the response**

In `TransactionsPage.tsx`, the month filter's state currently initialises to `""`. Seed it from the response's own month the first time one arrives, so the control and the list can never describe different months:

```tsx
  // Seeded from the response rather than from a locally computed "now": the
  // server decides which month the default request covers (parseTransactionFilter),
  // and a client that computed its own could disagree across a midnight or a
  // timezone. Only on the first response -- afterwards the user owns the value.
  const [monthSeeded, setMonthSeeded] = useState(false);
  useEffect(() => {
    if (monthSeeded || !data?.summary.month) return;
    setFilters((f) => ({ ...f, month: data.summary.month }));
    setMonthSeeded(true);
  }, [data?.summary.month, monthSeeded]);
```

Then add the way back to all-time, beside the input in `TransactionFilters.tsx`:

```tsx
        {/* The one way to widen the list to every month. Explicit, because a
            blank month input renders as Chrome's dashes and reads as broken
            rather than as "any month" -- which is the defect this task fixes,
            not a state to leave reachable by clearing the field. */}
        <button
          type="button"
          onClick={() => set("month", "")}
          className="min-h-11 self-end text-[11.5px] font-semibold text-accent sm:min-h-0"
        >
          All months
        </button>
```

Adjust names to the file's actual state shape (`data`, `setFilters` and `set` are the shapes the neighbouring code uses; read them before writing). The contract that matters: `""` in the control must reach the server as Task 7's `month=all`, never as an absent parameter.

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd web && npx vitest run src/features/money/TransactionsPage.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Verify in a real browser**

Open `/money/transactions`. The Month input must show the current month rather than dashes. Click "All months" and confirm the list widens, the header stops naming a month, and the input shows its cleared state deliberately.

- [ ] **Step 6: Commit**

```bash
git add web/src/features/money
git commit -m "fix(transactions): show the month the filter is on

The input opened blank, so Chrome drew its empty-month placeholder: a row of
dashes reading as a broken control. It now opens on the month the list is
scoped to, with an explicit way back to all months."
```

---

## Task 12: Land modal focus on the first field

**Files:**
- Modify: `web/src/components/Modal.tsx` (the open effect)
- Test: `web/src/components/Modal.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

Native `<dialog>`'s `showModal()` focuses the first focusable descendant. That is the header's ✕ button, because the header precedes the form. Measured live: opening "Log a transaction" from the keyboard lands on **Close**.

Everything else in this component is correct — the focus trap, Escape via `oncancel`, focus restoration on close, and the `supportsShowModal` guard. Change only where focus lands.

- [ ] **Step 1: Write the failing test**

```tsx
// showModal() lands on the first focusable descendant, which is the header's
// ✕ -- so opening a form modal from the keyboard put the cursor on Close.
it("lands focus on the first form field, not the close button", async () => {
  render(
    <Modal open onClose={() => {}} title="Log a transaction">
      <form>
        <label htmlFor="amount">Amount</label>
        <input id="amount" />
      </form>
    </Modal>,
  );

  await waitFor(() => {
    expect(document.activeElement).toBe(screen.getByLabelText("Amount"));
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd web && npx vitest run src/components/Modal.test.tsx
```

Expected: FAIL — the active element is the Close button.

- [ ] **Step 3: Focus the first field**

In `Modal.tsx`'s open effect, replace the current fallback focus step:

```tsx
    // showModal() focuses the first focusable descendant, which is the header's
    // ✕ -- so a keyboard user opening a form modal started on Close rather than
    // on the first thing to fill in. Prefer the panel's first form control, and
    // fall back to the platform's own choice when the panel has none (a
    // confirmation modal with only buttons should still land somewhere).
    const firstField = dialog?.querySelector<HTMLElement>(
      "input:not([type=hidden]), select, textarea",
    );
    if (firstField) {
      firstField.focus();
    } else if (dialog && !dialog.contains(document.activeElement)) {
      dialog.focus();
    }
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd web && npx vitest run src/components/Modal.test.tsx
```

Expected: PASS, with every pre-existing Modal test still passing — particularly the focus-restoration one.

- [ ] **Step 5: Mutation-check the test**

Change the selector to `"button"`. Re-run. Expected: FAIL. Restore and confirm PASS.

- [ ] **Step 6: Verify in a real browser**

jsdom does not implement `showModal()`, so the test proves the fallback path only. Open "+ Add transaction" in Chrome and confirm the caret is in Amount, then press Escape and confirm focus returns to the trigger button.

- [ ] **Step 7: Commit**

```bash
git add web/src/components
git commit -m "fix(modal): land focus on the first field, not on Close

showModal() focuses the first focusable descendant, which is the header's ✕.
Opening 'Log a transaction' from the keyboard started the user on Close.

Only the landing spot changes: the focus trap, Escape handling, focus
restoration and the supportsShowModal guard are untouched."
```

---

## Task 13: "<1% used" instead of "0% used"

**Files:**
- Create: `web/src/features/money/percentUsedCopy.ts`
- Modify: `web/src/features/money/budgetCopy.ts:22`
- Modify: `web/src/features/overview/copy.ts:34`
- Modify: `web/src/features/overview/BudgetCard.tsx:41`
- Modify: `web/src/features/money/BudgetPage.tsx` (the subtitle)
- Modify: `web/src/features/marriage/MoneyCheckInPanel.tsx:70`
- Test: `web/src/features/money/percentUsedCopy.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: `formatPercentUsed(percent: number, spentMinor: number): string` — exported from `percentUsedCopy.ts`, consumed by all three call sites.

`domain.PercentUsed` rounds to the nearest whole percent, so S$2.00 of S$800.00 renders `0% used`. **The rounding is correct and does not change** — the display was the lie, and both call sites already carry `spentMinor` alongside `percentUsed`, so no new field crosses the wire.

The copy is currently duplicated: `overview/copy.ts:34` and `money/budgetCopy.ts:22` are both `` `${percent}% used` ``, with three call sites. Per `hunting-sibling-defects`, all three are checked, not one.

- [ ] **Step 1: Write the failing test**

Create `web/src/features/money/percentUsedCopy.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { formatPercentUsed } from "./percentUsedCopy";

describe("formatPercentUsed", () => {
  // The defect: S$2.00 against S$800.00 is 0.25%, which domain.PercentUsed
  // correctly rounds to 0 -- and "0% used" reads as "nothing spent" to a
  // household that has spent.
  it("says <1% rather than 0% when something was spent", () => {
    expect(formatPercentUsed(0, 200)).toBe("<1% used");
  });

  // A household that genuinely has not spent must still read 0%, not "<1%",
  // which would be a different lie in the other direction.
  it("says 0% when nothing was spent", () => {
    expect(formatPercentUsed(0, 0)).toBe("0% used");
  });

  it("leaves every other figure alone", () => {
    expect(formatPercentUsed(66, 52800)).toBe("66% used");
    expect(formatPercentUsed(140, 112000)).toBe("140% used");
  });

  // Refunds can exceed the month's spend; PercentUsed is sign-aware and this
  // must not turn a negative into "<1%".
  it("leaves a negative figure alone", () => {
    expect(formatPercentUsed(-3, -2400)).toBe("-3% used");
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

```bash
cd web && npx vitest run src/features/money/percentUsedCopy.test.ts
```

Expected: FAIL — cannot resolve `./percentUsedCopy`.

- [ ] **Step 3: Write the helper**

Create `web/src/features/money/percentUsedCopy.ts`:

```ts
// The single definition of "N% used". It was written twice -- once in
// budgetCopy.ts and once in overview/copy.ts, both as `${percent}% used` --
// with three call sites between them, so fixing either one alone would have
// left the other standing. That is the sibling-defect shape docs/LEARNING.md
// records five times.
//
// domain.PercentUsed rounds to the nearest whole percent, which is right: it
// is the *display* of a rounded-to-zero figure that lies. S$2.00 against
// S$800.00 is 0.25%, renders as 0, and reads as "nothing spent" to a
// household that has spent. spentMinor is what separates that from a month
// where genuinely nothing has been spent, and both call sites already hold
// it -- so this needs no new field on the wire.
export function formatPercentUsed(percent: number, spentMinor: number): string {
  if (percent === 0 && spentMinor > 0) return "<1% used";
  return `${percent}% used`;
}
```

- [ ] **Step 4: Run the test to verify it passes**

```bash
cd web && npx vitest run src/features/money/percentUsedCopy.test.ts
```

Expected: PASS.

- [ ] **Step 5: Mutation-check the test**

Change the guard to `if (percent === 0)`. Re-run. Expected: the "says 0% when nothing was spent" test FAILS. Restore and confirm all four pass.

- [ ] **Step 6: Point all three call sites at the helper**

Delete `percentUsed` from `budgetCopy.ts` and `budgetUsed` from `overview/copy.ts`, then update each consumer to call `formatPercentUsed(percent, spentMinor)`:

- `overview/BudgetCard.tsx:41` — has `month.percentUsed` and `month.spentMinor`
- `money/BudgetPage.tsx` subtitle — has both on its month response
- `marriage/MoneyCheckInPanel.tsx:70` — read the check-in payload for `spentMinor`; if it is genuinely absent there, keep that call site on the plain `${percent}% used` form and say so in a comment rather than inventing a field

- [ ] **Step 7: Run the full frontend suite**

```bash
cd web && npx vitest run
```

Expected: PASS. Update any test asserting the literal string "0% used" on a month that has spending — that assertion was encoding the defect.

- [ ] **Step 8: Verify in a real browser**

On a household that has spent a small fraction of its budget, `/` and `/money/budget` must both read "<1% used". On a household that has spent nothing, both must read "0% used".

- [ ] **Step 9: Commit**

```bash
git add web/src/features
git commit -m "fix(budget): <1% used, rather than 0%, for a household that has spent

domain.PercentUsed rounds correctly; the display was the lie. S$2.00 of
S$800.00 rendered '0% used', which reads as 'nothing spent'.

The copy was duplicated in budgetCopy.ts and overview/copy.ts with three call
sites between them, so one fix would have left the other standing. Collapsed
to a single helper and all three call sites checked."
```

---

## Task 14: M2 browser walk and docs

**Files:**
- Modify: `docs/FEATURE_TRACKER.md`
- Modify: `docs/LEARNING.md`

- [ ] **Step 1: Walk the affected screens**

Desktop and 390px: `/money/transactions` (header matches list; month filter shows its month; empty submit shows Hearth's message; modal opens on Amount), `/` (achieved-goal card; "<1% used"), `/money/budget` ("<1% used"), sidebar and drawer (no ⌘K).

- [ ] **Step 2: Update `docs/FEATURE_TRACKER.md`**

Use the `hearth-product-driver` skill. Nothing new is *built* here, but rows describing screens whose behaviour was wrong should stop claiming a clean ✅ where they carried a defect. Recount the summary table rather than guessing; its columns must sum to the stated totals.

- [ ] **Step 3: Add the M2 entries to `docs/LEARNING.md`**

One entry per defect worth remembering. At minimum:

- **The Transactions month contract** — a handler whose doc comment stated a contract its own helper broke. What would have caught it: a test asserting the two halves of one response against each other, rather than each against a fixture.
- **The achieved-goal branch** — a component that guarded carefully against a state and then had no branch for it. Add as evidence to the existing zero-render pattern, which this makes at least the fifth instance.
- **Native validation as the first-line error surface** — the app's own messages existed and could not fire. Note that `noValidate` is absent from all fifteen forms and only one was fixed here.

- [ ] **Step 4: Run the full gate**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

Expected: green.

- [ ] **Step 5: Commit and open the PR**

```bash
git add docs
git commit -m "docs: what M2 taught"
git push -u origin ui-defects
gh pr create --title "M2 — seven defects" --body "$(cat <<'EOF'
The Transactions month contract, the achieved-goal card, Hearth's own
validation messages, the ⌘K chip, the month filter's opening value, modal
focus, and "<1% used".

Each reproduced in a browser before being fixed, and walked again after.
Spec: docs/superpowers/specs/2026-08-28-hearth-ui-polish-design.md

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01RbrEfoWQwDXmqQcbBp916b
EOF
)"
```

---

# M3 — Three consistency fixes

Branch off `main` once M2 has merged:

```bash
git checkout main && git pull && git checkout -b ui-consistency
```

Each of these is a place the app disagrees with itself, so each is arguable against the codebase rather than against taste.

## Task 15: Give "All caught up" the card surface its siblings have

**Files:**
- Modify: `web/src/features/money/BillsPage.tsx:490`
- Test: `web/src/features/money/BillsPage.test.tsx`

The panel currently renders as `rounded-xl bg-callout px-5 py-[18px]` — no border, no card background — while every sibling panel on the page sits on `bg-card` inside `border border-hairline`. Read the neighbouring `SubscriptionsCard` for the exact sibling classes rather than copying the string below blind.

- [ ] **Step 1: Match the sibling panels' surface**

```tsx
<div data-testid="bills-all-caught-up" className="rounded-xl border border-hairline bg-card px-5 py-[18px]">
```

Keep `bg-callout` **only** if the green tint is load-bearing for the "good news" read — in which case add the border and leave a comment saying the tint is deliberate. Decide by looking at the page, not from the diff.

- [ ] **Step 2: Run the Bills tests**

```bash
cd web && npx vitest run src/features/money/BillsPage.test.tsx
```

Expected: PASS. The `data-testid` is unchanged, so nothing should break.

- [ ] **Step 3: Verify in a real browser**

Load `/money/bills` on a household with everything paid. The panel must read as one of the page's cards.

- [ ] **Step 4: Commit**

```bash
git add web/src/features/money/BillsPage.tsx
git commit -m "fix(bills): give 'All caught up' the surface its siblings have

It sat on the bare canvas while every other panel on the page had a card
background and a hairline border, which read as an unfinished element rather
than as good news."
```

---

## Task 16: Weight Net as the conclusion it is

**Files:**
- Modify: `web/src/features/money/BreakdownCard.tsx:52-57`
- Test: `web/src/features/money/FinancesPage.test.tsx`

Net currently renders with `text-muted` on the label and the same `font-semibold text-ink` on the figure as every per-type row above it. Net is what those rows add up to, and should read as one.

- [ ] **Step 1: Raise the Net row's weight**

```tsx
      <div className="flex items-center justify-between border-t border-hairline pt-3 text-[13px]">
        {/* Net is the conclusion the rows above add up to, not another row in
            the list -- it carries the label colour and figure weight that say
            so, rather than the muted label a peer row gets. */}
        <span className="font-semibold text-ink">{FINANCES_COPY.net}</span>
        <span className="tabular text-[15px] font-semibold text-ink">
          {formatMoney(summary.netWorthMinor, summary.currency, symbol)}
        </span>
      </div>
```

The `tabular` class comes from M1 Task 1; if M1 has not merged, drop it and let Task 2's sweep pick this up.

- [ ] **Step 2: Run the Finances tests**

```bash
cd web && npx vitest run src/features/money/FinancesPage.test.tsx
```

Expected: PASS.

- [ ] **Step 3: Verify in a real browser**

Load `/money`. The Net row must read as a total, distinct from the rows above it, without having become a second heading.

- [ ] **Step 4: Commit**

```bash
git add web/src/features/money/BreakdownCard.tsx
git commit -m "fix(finances): weight Net as the total it is

It carried a muted label and the same figure weight as the per-type rows it
sums, so the conclusion read as another peer row."
```

---

## Task 17: Compose the Retros empty panel

**Files:**
- Modify: `web/src/features/marriage/RetrosPage.tsx:221`
- Modify: `web/src/features/marriage/retroCopy.ts:71`
- Test: `web/src/features/marriage/RetrosPage.test.tsx`

The detail panel renders one muted sentence in a 440px-tall empty card. The Overview's cards already show the shape to copy: a line saying what this is, and an action.

- [ ] **Step 1: Add the supporting copy**

In `retroCopy.ts`, beside `detailPlaceholder`:

```ts
  // The detail panel before a retro is picked. detailPlaceholder alone was one
  // muted sentence adrift in a 440px card; this pairs it with what the panel is
  // for, the shape the Overview cards already use.
  detailPlaceholderBody:
    "Each month's retro keeps its mood, its notes and the actions you agreed on.",
```

- [ ] **Step 2: Compose the empty state**

```tsx
              <div className="flex h-full flex-col items-center justify-center gap-1.5 px-8 text-center">
                <p className="text-[15px] text-ink">{RETRO_COPY.detailPlaceholder}</p>
                <p className="max-w-[42ch] text-[12.5px] leading-relaxed text-muted">
                  {RETRO_COPY.detailPlaceholderBody}
                </p>
              </div>
```

- [ ] **Step 3: Run the Retros tests**

```bash
cd web && npx vitest run src/features/marriage/RetrosPage.test.tsx
```

Expected: PASS. If a test asserts the placeholder's exact container, update it to assert the text.

- [ ] **Step 4: Verify in a real browser**

Load `/marriage/retros` without selecting a retro, at desktop and at 390px.

- [ ] **Step 5: Commit**

```bash
git add web/src/features/marriage
git commit -m "fix(retros): compose the empty detail panel

One muted sentence adrift in a 440px card. It now says what the panel is for,
in the shape the Overview cards already use."
```

---

## Task 18: M3 browser walk, docs, and PR

- [ ] **Step 1: Walk Bills, Finances and Retros** at desktop and 390px.

- [ ] **Step 2: Add the M3 entry to `docs/LEARNING.md`**

One entry: three panels each diverged from the surface, weight or empty-state shape their own siblings used. What would have caught it sooner: reviewing a new panel against the page it joins, not against the design file alone.

- [ ] **Step 3: Run the full gate**

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
make lint && make test
```

- [ ] **Step 4: Commit and open the PR**

```bash
git add docs/LEARNING.md
git commit -m "docs: what M3 taught"
git push -u origin ui-consistency
gh pr create --title "M3 — three consistency fixes" --body "$(cat <<'EOF'
Bills' "All caught up" gets a card surface, Finances weights Net as a total,
and the Retros detail panel gets a composed empty state.

Each is a place the app disagreed with itself. Spec:
docs/superpowers/specs/2026-08-28-hearth-ui-polish-design.md

🤖 Generated with [Claude Code](https://claude.com/claude-code)

https://claude.ai/code/session_01RbrEfoWQwDXmqQcbBp916b
EOF
)"
```
