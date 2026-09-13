# Whole-codebase review — 2026-09-13

**Scope:** every production file under `api/` (Go, excluding generated
`sqlcgen/`) and `web/src/` (React + TypeScript), about 44k and 30k lines.
Test files were read where they exposed a design problem.
**Asked for:** architecture, SOLID, readability, dead code, and any other
engineering principle worth applying.
**Branch:** `review/codebase-cleanup` (uncommitted).

## Fix status (2026-09-13, same day)

The product owner asked for everything below to be fixed, taking each
recommendation as written. The table says what was done for each finding,
how it was proven, and the one item deliberately left.

| § | Finding | Status | How it was proven |
|---|---|---|---|
| 1.1 | Bill default in the handler | ✅ `MarkPayment.AmountMinor` is `*int64`; `BillService.MarkPaid` pays the bill's own amount when none is sent | New usecase test, mutation-checked |
| 1.2 | "Find one row by re-reading the list" | ✅ `BillService.View` and `GoalService.View`; `listBillViews`, `findBillViewByID`, `listGoalViews`, `findGoalViewByID` deleted | Tests per `View`; HTTP suite green |
| 1.3 | API trusted client-IP headers from anyone | ✅ `trustedProxyRealIP` replaces chi's `RealIP`: `X-Real-IP` only from a peer inside `TRUSTED_PROXY_CIDRS`, never `True-Client-IP` or `X-Forwarded-For`; unset trusts nobody; production compose sets `172.28.0.0/16`. The audit log IP now uses `clientIP` | One test per case, mutation-checked |
| 2.1 | Fail-closed rule not followed | ✅ `transaction.go` `default` added. **Sibling hunt found one more:** an unknown `SMTP_TLS_MODE` meant *no* TLS; it now means mandatory TLS. Enum reads from the database now parse: category kind, account type, transaction kind, role, capabilities | TLS test and a no-database enum-read test, both mutation-checked |
| 2.2 | "Compose a screen" methods do too many jobs | ✅ `bill.List`, `budget.Month`, `goal.List` split into named helpers, every comment moved with its code | usecase suite green |
| 2.3 | 9-method port for one call | ✅ `GoalLookup` (one method) for `BudgetDeps.Goals` | Compiles against the narrow type |
| 2.4 | 523-line `MapDomainError` switch | ✅ Ordered table `domainErrorResponses` (125 rows, original order, comments kept) | A temporary test ran old and new side by side on 16,645 error shapes, all equal; ~105 response assertions green |
| 2.5 | Eight copied constraint cases in `translate` | ✅ `uniqueConstraintErrors` map | Postgres suite green |
| 2.6 | Flat modal props force `!` assertions | ✅ `BillModal`/`GoalModal` props are discriminated unions; every `bill!.`/`goal!.` gone; GoalModal carries the target inside its suggestion; BudgetModal save loop has no `!` (rows are a union) | tsc; grep for `!.` empty |
| 2.7 | Unchecked `as` casts on `<select>` values | ✅ `parseEnum` at all 6 sites | Unit test, mutation-checked |
| 3.1 | `RetroModal`, `BudgetModal`, `VisionModal` too large | ✅ RetroModal 698 → 262 lines (`useRetroDraft`, `CarryOverList`, `AddActionComposer`, `DiscardDraftControl`); BudgetModal 705 → 417 (`useBudgetRows`, `BudgetCategoryRow`); VisionModal 845 → 327 (`useVisionDraft`, editors in their own files) | `RetroModal.test.tsx` unchanged and green |
| 3.2 | Modal building blocks copied across files | ✅ `ModalActions` 12/12 sites; `useConfirmAction` 6/6; `Field` 58 sites in 19 files; `FIELD_CONTROL_CLASS` 58 uses. Sites left on purpose are those whose markup differs (a label wrapping its input, screen-reader-only labels, a label with a link beside it) | 887 → 894 tests; hook mutation-checked |
| 3.3 | Small frontend duplications | ✅ `LoadingScreen` for the 8 router fallbacks; `monthLabel` in `month.ts`; literal query keys replaced with exported builders; `apiErrorMessage` moved to `src/api/errorMessage.ts`; settings hooks moved to `use*.ts` files; `fetchAndParse` for 64 fetch-then-parse pairs; `usePendingBillActions` and `resolveTemplatePrefill` extracted from the Bills and Budget pages | 874 → 887 frontend tests, green |
| 3.4 | Go handler and repository repetition | ✅ One row converter per entity (via `sqlc.embed`); `parseOptionalUUIDFilter`; one date-parse helper for four parsers; the sign-in failure tail shared by one helper (decoy password checks untouched). **Not done by design:** the generic `setXArchived` handler — this review recommended waiting for a fifth archivable resource | Postgres, HTTP and usecase suites green |
| 4.2 | Unreachable `RecentAudit` chain; unused `setRetros` | ✅ Both deleted. Tests that read the audit log through `Recent` now query `admin_audit_log` directly, so `Record` stays proven | Postgres and HTTP suites green |
| 5.1 | Nothing checks for dead code | ✅ `make lint-dead` (deadcode, staticcheck, knip — pinned versions) is now part of `make lint`; staticcheck findings fixed first | Each tool prints nothing |
| 5.3 | Docs and repository hygiene | ✅ `CLAUDE.md` Go-version note; merged worktree `.claude/worktrees/transactions` removed; merged branches `worktree-transactions`, `feat/interim-overview`, `feature/budget`, `fix/ux-repair` deleted. `net-worth-trend` (unmerged) left alone | — |

Docs updated in the same change: `docs/SYSTEM_DESIGN.md` (ports table,
middleware pipeline, proxy and audit-IP notes, frontend structure),
`docs/HANDOVER.md` (the RealIP open item resolved, dev limiter behaviour),
`docs/FEATURE_TRACKER.md` (audit-screen row), `docs/LEARNING.md`, and a dated
note on `docs/adr/0002-first-production-host.md`.

### Found and fixed along the way

| What | Fix | Proof |
|---|---|---|
| An unknown `SMTP_TLS_MODE` fell back to no TLS | Falls back to mandatory TLS | Test, mutation-checked |
| A failed retro discard showed its error only after a second click | The error line sits below the whole control | New `DiscardDraftControl.test.tsx`, mutation-checked |
| Double-clicking Remove in either Holding panel sent two DELETEs | The confirm button is disabled while its request runs | One test per panel, mutation-checked |

### Verified on the finished tree

| Check | Result |
|---|---|
| `make lint` (architecture lint, `tsc`, eslint, deadcode, staticcheck, knip, `go vet`) | Pass |
| `make test-api` (all 14 Go packages, including the Docker-backed `adapter/http` and `adapter/postgres`) | Pass |
| Frontend tests (`vitest`) | Pass: 101 files, 897 tests (874 before this work) |
| First browser walk against the dev stack (colima) | **Did not test the frontend fixes.** It was recorded as a pass: every page rendered, a bill was marked paid, the edit modals opened pre-filled, a budget was saved from a template, a retro was created, saved and discarded, and both Holding confirm pairs were opened. But Vite in `hearth-web-1` was still serving modules compiled before the fixes (container started 02:52Z, `DiscardDraftControl.tsx` edited 02:59Z). So the discard-error and Holding double-click fixes were not what the browser ran. The September budget this walk saved is still in the dev database |
| Second browser walk, after `docker restart hearth-web-1` | Pass, with real mouse and keyboard input (Playwright, then Chrome DevTools). **Bills:** marked paid (`POST` 200, next due moved a month), edited the due date (`PATCH` 200), and double-clicked "Undo payment", which sent exactly one `DELETE` (204) and removed the payment row and its ledger transaction. **Retro:** chose a mood and typed into a field, and Save draft stored both (`PATCH` 200, version 2). With the `DELETE` forced to 500 in the page, the error showed straight away with "Discard draft" back and the retro kept; the real discard then returned 204. **Holding income:** recorded a row (201), then double-clicking "Really remove" sent exactly one `DELETE` (204). **Modals:** each one checked is `:modal`. Escape returns focus to the button that opened it. In the Vision modal, the page behind refuses focus and the split pillar editor takes typed input. No console errors apart from `favicon.ico` 404. Every row the walk created was removed, and the bill is back to next due 2026-10-01 |
| What the first walk could not have tested | Only files edited after the containers started (02:52:27Z) were at risk: `DiscardDraftControl.tsx`, `HoldingIncomePanel.tsx`, `HoldingLotsPanel.tsx` and `useConfirmAction.ts`. Everything else the first walk saw was current, including the Budget and Goal modals. No `.go` or `.sql` file changed after that time, and `air` built once at 02:52:27Z, so both walks ran the branch's API |
| Not covered by the walk | The Budget and Goal modals were not reopened after the restart, but they were not stale. Failing-server paths other than retro discard are covered by unit tests only. The Vision modal closes on Escape without asking about unsaved edits, but the pre-split version did the same, so this pass did not change it |

## How this was done

1. **Tools first, opinions second.** `deadcode` (unreachable Go functions),
   `staticcheck` (Go bugs and unused code) and `knip` (unused TypeScript files,
   exports and packages) were run over the whole tree. None of the three is in
   `make lint`, so none had ever run.
2. **Every tool hit was checked by hand** before being called dead. Most knip
   hits turned out to be "used, but only inside its own file", which is not the
   same thing (see *Dead code* below).
3. **Two focused reviewers** read the largest and most-changed files: one for
   the Go layers, one for the React features. Their findings were then
   spot-checked against the code; only verified findings are listed.
4. `CLAUDE.md`, the ADR titles and the `docs/LEARNING.md` patterns were read
   first, so nothing below recommends something the project already decided
   against without saying so.

## The short version

**The architecture is in good shape.** The dependency rule holds, and not only
at the import level `arch-lint.sh` checks: no database, HTTP or third-party
*concept* leaks into `domain` or `usecase` either. Ports are already narrow in
several places (`HoldingCounter`, `CategoryLookup`, `AccountLookup`). Transaction
handling is consistent. Nesting is shallow everywhere. There are no `any` types
and no broken React effects.

**No CRITICAL or HIGH defects were found.** What was found falls into four groups:

| Group | What it looks like | Worst example |
|---|---|---|
| Rule breaks | A project rule from `CLAUDE.md` is not followed in a few places | A database value is cast straight to a Go type with no check (`category_repo.go:132`) |
| Logic in the wrong layer | A default or a lookup lives in a handler instead of the service | Marking a bill paid re-reads the whole bills page just to find one amount |
| Long functions | Functions that do 4–6 jobs | `BillService.List` is 208 lines; `RetroModal` is one 698-line component |
| Repetition | The same few lines copied 4–14 times | A form field wrapper repeated in 14 files |

Dead code was small and has been removed (see *What was changed*).

---

## 1. Architecture

### 1.1 A default that belongs to the service lives in the HTTP handler — MEDIUM
`api/internal/adapter/http/bill_handlers.go:372-391`

When a caller marks a bill paid without an amount, the handler fetches the
**whole bills page** (`listBillViews`: four repository calls plus the page
calculation) just to read that one bill's stored amount. `BillService.MarkPaid`
(`usecase/bill.go:682`) already loads the bill and holds that amount.

- **Why it matters:** a rule ("no amount means the bill's own amount") is
  hidden in the adapter, so the CLI or Telegram could not reuse it. Every
  such request also does a full page query it does not need.
- **Fix:** make `MarkPayment.AmountMinor` a `*int64`, default it inside
  `MarkPaid`, and delete the handler's branch. **Cost:** S–M. **Risk:** low.

### 1.2 "Find one row by re-reading the list" is repeated — MEDIUM
`bill_handlers.go:374, 410, 582, 601` and `goal_handlers.go:267, 459, 492`

Neither `BillService` nor `GoalService` offers a single-item read, so six
handlers fetch the whole list and search it. This is the root cause of 1.1.
The `goal_handlers.go:266-282` currency check is documented as deliberate and
can stay.

- **Fix:** add a single-item read (`BillService.View(ctx, household, id)`)
  that reuses the existing view calculation. **Cost:** M.

### 1.3 The API trusts client-IP headers and relies on nginx to clean them — MEDIUM (defence in depth)
`api/internal/adapter/http/router.go:121`, `middleware_ratelimit.go:82`

`chi`'s `middleware.RealIP` is deprecated because it believes
`True-Client-IP`, `X-Real-IP` and `X-Forwarded-For` from anyone. The rate
limiter and the admin audit log both key on the address it produces.

**Checked: production is safe today.** `deploy/Caddyfile` replaces
`X-Forwarded-For`. `web/nginx.conf:45-47` trusts it only from the Docker
network. nginx then blanks `True-Client-IP` and sets `X-Real-IP` from the real
peer (`nginx.conf:98-99, 117-118`). The api port is not published in
`deploy/docker-compose.prod.yml`. The comments there explain all of this well.

- **The gap:** the safety lives in two config files *outside* the service. Put
  a different proxy in front, or expose the port, and a caller can pick their
  own IP. That bypasses the per-IP sign-up limit and writes a false IP into the
  audit log. `middleware_admin.go:117-128` already warns about this.
- **Fix:** replace `middleware.RealIP` with a ~30-line middleware that reads
  `X-Real-IP` only when `r.RemoteAddr` is inside a configured trusted range
  (`TRUSTED_PROXY_CIDR`, default `172.28.0.0/16`). That also removes the only
  `staticcheck` deprecation warning. **Cost:** S–M. **Risk:** low, with a test
  per header.

### 1.4 Things checked and found correct (no action)

- **`usecase/seed.go`** looks like an ops tool in the wrong layer, but it
  imports only `domain`, uses only ports, and is called only from
  `cmd/adminctl`. That is dependency inversion working; LEARNING pattern 14
  already settled where seed data lives.
- **`router.go` `NewRouter` (447 lines)** should stay one function in one file.
  ADR 8 depends on every route's guards being visible in one read.
- **`cmd/api/main.go` `run` (447 lines)** is flat wiring, which is its stated
  job. A `wireRepositories(db)` helper could save ~30 lines. Cosmetic.
- **`usecase/ports.go` (1,884 lines)** is one contract file by choice, with
  load-bearing comments. Splitting it by area inside the same package is
  possible but low value.
- **`holding_repo.go` `InsertWithFold`/`DeleteWithFold`** pass a callback so a
  recalculation runs inside the row lock. This is a good pattern and a
  precedent for others.
- **Frontend:** no component calls `apiFetch` outside a hook. The one
  exception (`TransactionsPage.tsx:60-68`) is deliberate and explained.

---

## 2. SOLID

### 2.1 Fail-closed rule not followed in two places — MEDIUM (single responsibility / Liskov)
`CLAUDE.md`: *"A switch over a type that arrives from a database column or a
request needs a default that refuses."*

1. **`api/internal/adapter/postgres/category_repo.go:132` and `:196`** build
   `domain.CategoryKind(c.Kind)` straight from the database string.
   `domain.ParseCategoryKind` (`domain/category.go:22`) exists for exactly this.
   It refuses unknown values, has its own test, and is **called by nothing in
   production**. The database `CHECK (kind IN ('expense','income'))` saves us
   today; the Go code does not.
   **Fix:** call `ParseCategoryKind` in both places and return its error.
   `toCategory` then needs to return an error, so its four callers change.
   **Cost:** S. **Needs:** the Postgres test suite (Docker), which is why it
   was not applied here.
2. **`api/internal/usecase/transaction.go:243-256`** switches on `t.Kind` with
   no `default`. It is safe today only because `ParseTransactionKind` ran two
   lines earlier. Add a fourth kind and this check silently does nothing.
   **Fix:** a `default:` that returns an error. **Cost:** trivial.

### 2.2 "Compose a screen" service methods do too many jobs — MEDIUM (single responsibility)
`usecase/bill.go:201` `List` (208 lines), `usecase/budget.go:162` `Month`
(200 lines), `usecase/goal.go:145` `List` (130 lines)

Each one fetches, classifies every item, keeps several running totals, finds
"the next one" with tie-breaking, and builds the view, all in one body.
`budget.go:374` `buildCategoryViews` shows the project already knows the fix
and used it once.

- **Fix:** extract the classifier and each total into named helpers
  (`sumDueAndPaid`, `findNextDueBill`, `spentByPersonAndCategory`), moving each
  comment with its code. **Cost:** M. **Risk:** low–medium; covered by tests.

### 2.3 A 9-method port injected for one call — MEDIUM (interface segregation)
`usecase/budget.go:133`

`BudgetDeps.Goals` is the full `GoalRepository`, but `BudgetService` calls only
`Goals.Get`, once (`budget.go:581`). `HoldingCounter` is the house precedent for
carving out a narrow port.

- **Fix:** a one-method `GoalLookup` port. `*GoalRepo` already satisfies it.
  **Cost:** S.

### 2.4 `MapDomainError` is one 523-line switch — LOW–MEDIUM (open/closed)
`api/internal/adapter/http/errors.go:112-635`

Every new error in any feature edits this function. About 40 of its cases are
the same shape: `errors.Is(err, X)` then `WriteError(status, code, message)`.

| Option | Pros | Cons |
|---|---|---|
| A. Leave it | Boring and greppable, which `CLAUDE.md` values near security code | Grows with every feature; merge conflicts |
| B. Move the ~40 one-line cases into an ordered slice `[]struct{err; status; code; message}` and keep a switch for the ~6 cases with bodies, logging or `errors.As` | One line per new error; order is kept, so wrapped errors still match the way they do now | A table is one step less obvious than a switch |
| C. One error mapper per feature, registered at start-up | Features own their own copy | More moving parts; a missed registration fails at runtime, not compile time |

**Recommendation: B.** It is still boring, keeps the order, and turns a
500-line function into a readable table. Do it when the HTTP suite can run.

### 2.5 The eight unique-constraint cases in `translate` are copies — LOW (open/closed)
`api/internal/adapter/postgres/translate.go` (moved there in this change)

Eight `case` lines differ only in the constraint name and the error. A
`map[string]error` from constraint name to domain error, with each comment
beside its entry, would cut about 40 lines. A new unique key would then add a
map entry instead of a `case`. **Cost:** S.

### 2.6 Frontend: flat props force non-null assertions — MEDIUM (Liskov-style type safety)
`web/src/features/money/BillModal.tsx` (4 × `bill!.`) and `GoalModal.tsx`
(2 × `goal!.`)

Props are `{ mode: "create" | "edit"; bill?: Bill }`, so TypeScript cannot know
`bill` exists in edit mode. LEARNING's Frontend catalogue already fixed this
once for the `Summary` type with a discriminated union.

- **Fix:** `{ mode: "create" } | { mode: "edit"; bill: Bill }`. **Cost:** M.

### 2.7 Frontend: unchecked `as` casts on `<select>` values — LOW
Six places: `InviteMemberModal.tsx:118`, `AccountModal.tsx:294`,
`BillModal.tsx:392`, `HoldingModal.tsx:124`, `HoldingIncomePanel.tsx:90`,
`HoldingLotsPanel.tsx:105`

Low risk today because the options are fixed, but it is the fail-closed rule
again. `VisionModal.tsx:190` shows the safe inline form.

- **Fix:** a small `parseEnum(value, allowed, fallback)` helper. **Cost:** S.

---

## 3. Readability and repetition

### 3.1 `RetroModal.tsx` is one 698-line component — MEDIUM
`web/src/features/marriage/RetroModal.tsx:40-697`, 12 `useState` calls, no
sub-components

The seams are clear:

- `CarryOverList`: state 101–118, handler 237–249, markup 467–504
- `AddActionComposer`: state 126–129, handlers 251–289, markup 506–591
- `DiscardDraftControl`: state 136–138, handler 302–316, markup 618–670
- `useRetroDraft(month, retro)`: state 77–101, seed effect 140–148, handlers 155–220

**Cost:** L, because `RetroModal.test.tsx` relies on exact `data-testid`s.
`BudgetModal.tsx` (extract `useBudgetRows` and a `BudgetCategoryRow` for lines
552–625) and `VisionModal.tsx` (extract `useVisionDraft` for lines 456–629) have
the same problem at a smaller size. **Cost:** M each.

### 3.2 Modal building blocks copied across many files — MEDIUM
`components/FieldPair.tsx` sets the project's own bar for extracting a shared
component ("twelve call sites across seven files"). These are already past it:

| Copied pattern | Copies | Shared piece to add |
|---|---|---|
| Label + input wrapper with the same Tailwind classes | 14+ files | `<Field label htmlFor>` |
| Cancel / Save footer (`mt-1 flex gap-2.5`) | 12 files | `<ModalActions>` |
| Ask → confirm → cancel for a destructive action | 6 files | `useConfirmAction()` |

**Cost:** M each. Existing tests check text and roles, not markup, so they
protect the change.

### 3.3 Small duplications — LOW, cost S each

- `routes/router.tsx` writes the same "Loading…" `Suspense` fallback 8 times;
  `AdminShell.tsx:19` `AdminLoadingScreen` already exists. Importing a
  component is allowed by the react-refresh rule; only defining one is not.
- `monthLabel` is identical in `TransactionsPage.tsx:75` and `BudgetPage.tsx:70`.
  Move it to `features/money/month.ts`.
- `InviteMemberModal.tsx:34` invalidates the literal `["household","members"]`
  instead of importing `householdMembersQueryKey`. Today they match; if the
  key changes, this silently stops refreshing (LEARNING pattern 22).
- `apiErrorMessage` lives in `features/auth/copy.ts` but is a general helper
  imported by 16 files in other features. Move it next to `api/client.ts`.
  **Cost:** M (import paths only).
- Seven settings components define their hooks inline, while money, marriage
  and admin keep them in `use*.ts` files, and four pages say so in comments.
  Pick one rule.
- `apiFetch<unknown>(url)` followed by `schema.parse(...)` is written out
  dozens of times in hooks. A `fetchAndParse(url, schema, init?)` helper would
  make each one a single line. Do not template the `invalidateQueries` calls
  next to them: LEARNING says each set is a deliberate per-endpoint choice.

### 3.4 Go handler and repository repetition — LOW–MEDIUM

- **Row to domain, three times per entity:** `account_repo.go:203, 220, 237`
  and `transaction_repo.go:170, 324, 341` each rebuild the same struct field by
  field from three different sqlc row types. `goal_repo.go:346-351` `toGoal`
  already shows the fix, with a comment giving this exact reason: take plain
  fields, not a generated row struct. **Cost:** M.
- **Optional UUID query filters:** `transaction_handlers.go:211-234` repeats
  the same 7-line check three times. Add `parseOptionalUUIDFilter`. **Cost:** S.
- **Four date parsers** (`parseBillDueDate`, `parseBudgetMonth`,
  `parseGoalMonth`, `parseHoldingDate`) differ only in layout and message.
  **Cost:** S.
- **Four `setXArchived` handlers** have the same ~10 lines. A generic helper is
  possible. **Recommendation:** wait for a fifth archivable resource; four
  copies of ten lines is not yet worth a generic.
- **`auth.go:216-252` and `:267-281`** repeat record-failure → re-count →
  evaluate. That tail can be shared. Do **not** move the decoy password check;
  its position keeps timing identical, as the comments explain.

---

## 4. Dead code

### 4.1 Removed in this change (verified dead)

| What | Where | Evidence |
|---|---|---|
| `react-hook-form`, `@hookform/resolvers` | `web/package.json` | Zero imports. `npm uninstall` then worked without `--legacy-peer-deps`, so the Makefile comment about that flag was stale too and was removed |
| 12 unused types (`User`, `Membership`, `Agreement`, `Trend`, `VisionResponse`, `CategoriesResponse`, `UpdateMemberResponse`, `AdminDatabaseTable`, `HoldingEvent`, `HoldingValuation`, `HoldingIncome`, `ReturnComponent`) | web schemas | Referenced once: their own declaration |
| `OUTBOX_MAX_LIMIT`, `holdingResponseSchema` | `useAdminOutbox.ts`, `holdingSchemas.ts` | No references. Only 3 of 62 mutations parse their response, so not parsing is the house pattern, not a missing check |
| 3 re-exports nobody imported | `useHoldings.ts`, `useAdminDatabase.ts`, `useAdminDirectory.ts` | knip, then comments updated to match |
| `(*client).String` | `api/cmd/hearthctl/client.go` | `deadcode -test`: unreachable even from tests |
| 4 test-double methods (`readLog.reset`, three `mailerDouble` counters) | `api/internal/usecase/testdouble_test.go` | staticcheck U1000 |

**Over-exported, not dead:** 44 frontend symbols were used only inside their own
file. Their `export` keyword was removed, not the code. A reader can now trust
that `export` means someone outside imports it. One comment that claimed
*"Exported because AccountModal's toMinorUnits reads the same set"*
(`formatMoney.ts`) was wrong, because `toMinorUnits` now lives in that same
file. It was corrected.

### 4.2 Found and deliberately left — owner decision needed

**`AdminService.RecentAudit` and everything below it.** That is the service
method (`usecase/admin.go:187-209`), the port method
(`ports.go:402` `AdminAuditRepository.Recent`), the Postgres implementation
(`admin_repo.go:168-190`), the query (`queries/admin.sql:46-48`) and their tests.
Nothing in production reaches any of it. The audit screen was cut on
2026-09-02, and `docs/FEATURE_TRACKER.md:1886` records that
*"`AdminService.RecentAudit` stays in place for the tests that use it."*

| Option | Pros | Cons |
|---|---|---|
| A. Delete the whole chain, and update `SYSTEM_DESIGN.md:814` and the tracker row | Less code to maintain; LEARNING pattern 15 ("a capability nobody can reach is not shipped"); tests that exist only to test unused code protect nothing | Reverses a recorded decision; the chain would need rewriting if the screen returns |
| B. Keep it (today) | No work; honours the record | About 90 lines of production code plus tests with no user |

**Recommendation: A.** The tracker row itself says bringing the screen back
means rebuilding from the spec, so keeping the back half saves little. This
reverses a recorded decision, so it is the owner's call.

**`retroActionRepoDouble.setRetros`** (`testdouble_test.go:3265`) is never
called, so the `retros` field it sets is always `nil` and part of the double's
`Add` never runs. Deleting only the setter would leave a misleading field and
comment. Remove the field and that branch of `Add` together. **Cost:** S.

### 4.3 Kept on purpose (tools flag these, but they are fine)

- `internal/testsupport/*`: test support by design.
- `openrouter.WithBaseURL`, `domain.TokenState.String`: used only by tests.
  These are normal test hooks and debugging aids.
- `prettier` in `devDependencies`: knip cannot see that `make fmt` runs it.
- staticcheck SA4000 in `middleware_ratelimit_test.go:17`: a false positive.
  Calling `allow` twice is the point of the test.

---

## 5. Other engineering principles

### 5.1 Nothing checks for dead code — MEDIUM (the finding that prevents the next round)
None of `deadcode`, `staticcheck` or `knip` runs in `make lint`, which is why
every finding in section 4 could build up.

| Option | Pros | Cons |
|---|---|---|
| A. Add all three to `make lint` now | Stops new dead code at once | staticcheck first needs 1.3 fixed, or a `//lint:ignore`, plus a few style fixes in tests |
| B. Add `make lint-dead` now (`deadcode -test` + `knip`, both clean after this change) and promote it into `lint` once staticcheck is clean | Green on day one; no false starts | Two steps |
| C. Leave as is | No work | This review becomes necessary again |

**Recommendation: B.** Pin exact versions (LEARNING pattern 7). Run the Go tools
with `GOTOOLCHAIN=go1.25.7`: `go run tool@version` builds with the `go` on
`PATH` (1.24.2) and fails on every package. Add a `knip.json` that ignores
`prettier`.

### 5.2 A test that could not fail on its second half — fixed
`api/cmd/hearthctl/cmd_import_test.go:175` ignored both the error and the JSON
decode of its second run (staticcheck SA4006). If the second run crashed, the
test read the *first* run's leftover summary. It now checks both. **Proven by
mutation:** making a run with replayed rows exit 0 turns the new assertion red,
and restoring the code turns it green.

### 5.3 Docs and repository hygiene — LOW

- `CLAUDE.md` says `go` lives at `go-v1.24.2`, but `api/go.mod` requires
  `go 1.25.7`. `make` works because `GOTOOLCHAIN=auto` downloads 1.25.7 inside
  the module, but anything run outside the module fails with *"package requires
  newer Go version go1.25"*. Update the note.
- `.claude/worktrees/transactions` is merged and 7 weeks old. Safe to remove
  with `git worktree remove`. `.worktrees/net-worth-trend` is **not** merged
  (4 weeks old), so that one is the owner's call.
- Local branches `feat/interim-overview`, `feature/budget` and `fix/ux-repair`
  are fully merged and safe to `git branch -d`. The other 19 show as unmerged
  but are probably squash-merged. Check each before deleting, because a
  deleted unmerged branch is gone.

---

## What the first pass changed (history; the final state is in Fix status above)

All changes are either deletions or moves that change no behaviour.

**Go**
- `adapter/postgres/translate.go` (new): `translate` and its constraint-name
  constants, moved unchanged out of `user_repo.go`. About 180 callers in every
  repository used it, but it lived in the user repository's file. Same
  package, so no caller changed.
- `cmd/hearthctl/client.go`: removed the unused `String` method.
- `cmd/hearthctl/cmd_import_test.go`: the second run's exit code and output are
  now checked (5.2).
- `usecase/testdouble_test.go`: removed four unused methods.

**Web**
- Removed two unused packages, 12 unused types, 2 unused constants and 3 unused
  re-exports. 44 file-local symbols lost `export`. One wrong comment was fixed.
- `mutationObserverCallbacks.test.ts` imported `@tanstack/query-core`, which
  is not a declared dependency. It now imports the same classes from
  `@tanstack/react-query`, which re-exports them unchanged.

**Docs and build**
- `Makefile`: removed the stale `--legacy-peer-deps` comment.
- `docs/LEARNING.md`: one Tooling entry on the sweep and its traps.
- No row in `docs/FEATURE_TRACKER.md` changed, and no port, route or table
  changed, so `docs/SYSTEM_DESIGN.md` is unaffected.

## Validation of the first pass (history)

| Check | Result |
|---|---|
| `make lint` (architecture lint, `tsc`, eslint, `go vet`) | Pass |
| Frontend tests (`vitest`) | Pass: 96 files, 874 tests |
| `go test ./cmd/hearthctl/ ./internal/usecase/` | Pass |
| Mutation check on the hardened import test | Red when broken, green when restored |
| `deadcode -test`, `knip` after the change | Only the items in 4.2 and 4.3 remain |
| `go test` for `adapter/postgres` and `adapter/http` | **Not run.** They need Docker, and no daemon was running. The only change in those packages is the `translate` move, which the compiler fully checks |
| Browser walk | **Not done**, for the same reason: the stack needs Docker. No runtime path changed (only unused code and `export` keywords), but `CLAUDE.md` asks for a walk, so run `make dev` and click through Money and Marriage before merging |

## Suggested order of work (history; all done, see Fix status)

1. **Quick, safe fixes (half a day):** the fail-closed fixes (2.1), the bill
   default moved into the service (1.1), the `GoalLookup` port (2.3), the
   `InviteMemberModal` query key, `monthLabel`, and the router fallback.
2. **Tooling (an hour):** `make lint-dead` (5.1).
3. **Security hardening (half a day):** the trusted-proxy middleware (1.3).
4. **Owner decision:** delete or keep the `RecentAudit` chain (4.2).
5. **Readability, one per change:** `RetroModal` split (3.1), shared modal
   pieces (3.2), discriminated-union props (2.6), service helpers (2.2),
   repository row conversion (3.4), `MapDomainError` table (2.4).
