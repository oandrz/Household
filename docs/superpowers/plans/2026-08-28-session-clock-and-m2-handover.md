# Handover — the session-clock bug, then M2

Written 2026-08-28 by the agent that executed M1 of the UI-polish plan, for whoever
picks up the next two pieces of work. It assumes you have no context beyond this
repository.

There are two jobs here, and **the order matters**: fix the session clock first,
because M2's only Go change lands in the same package as the failing test, and you do
not want to work in a suite that is already red.

---

## 0 · Where things stand

- **M1 is done and open as a pull request:** <https://github.com/oandrz/Household/pull/10>,
  branch `ui-polish`, 21 commits ahead of `main`. It is frontend-only — 19 files, all
  under `web/`, no Go.
- **The spec both milestones implement:**
  `docs/superpowers/specs/2026-08-28-hearth-ui-polish-design.md`
- **The plan:** `docs/superpowers/plans/2026-08-28-hearth-ui-polish.md`. M1 is Tasks
  1-6 (done). **M2 is Tasks 7-14.** M3 is Tasks 15-18.
- **`make test` is currently red on `main`.** That is job 1 below. `make lint` passes.
- M1's execution ledger — every ruling, every measurement, every browser check — is at
  `.superpowers/sdd/2026-08-28-hearth-ui-polish/progress.md`. It is git-ignored scratch,
  so read it before anything deletes it. It is the record of *why* things are the way
  they are.

---

## 1 · Job one: the session-clock bug

### What is broken

`TestOwnerSeesTheTwelveMonthTrend` (`api/internal/adapter/http/accounts_api_test.go:253`)
fails, and has failed since 2026-08-27. It will fail every day from now on.

The failure is **not** about the trend. It is:

```
accounts_api_test.go:257: create account: status = 401,
  body = {"error":{"code":"UNAUTHENTICATED","message":"Sign in required."}}
```

### Why

1. `SessionTTL = 30 * 24 * time.Hour` — `api/internal/adapter/http/middleware_session.go:41`.
2. The test pins its clock to an **absolute past date**:
   `&movableClock{now: time.Date(2026, 7, 28, 9, 0, 0, 0, time.UTC)}`.
   The session it signs in with therefore carries `expires_at = 2026-08-27`.
3. Session validation is **SQL, not Go**. `GetLiveSession`'s `WHERE` includes
   `expires_at > now()` — *Postgres's* `now()`, real wall time.
   `api/internal/adapter/postgres/session_repo.go:25-27` states this outright:
   "ByTokenHash relies on GetLiveSession's own WHERE clause … there is no separate
   check here."
4. The injected `movableClock` cannot reach that guard. Once real time passed
   2026-08-27, the session was expired before the test's first authenticated request.

The test passed for the thirty days after it shipped in `77123b9` ("Net worth,
twelve-month trend (#9)") because the pinned date was within one TTL of real time,
then became permanently red.

Its own doc comment claims the pinned clock makes the window "a known, reproducible
range rather than whatever month the suite happens to run in." Reproducibility is
exactly what it does not have.

### The fix, and why this one

**Do not change the production SQL.** `expires_at > now()` evaluated in Postgres is a
sound design: one clock, no app/database skew. Changing it to app-supplied time to
suit a test would trade a real production property for a test convenience.

**The model to copy already exists in the same package, one file over.**
`TestSessionCookiesSlideWhenExtended` (`api/internal/adapter/http/auth_api_test.go:331`)
also uses `movableClock` and does not rot, because it anchors to real time and moves
*forward*:

```go
clk := &movableClock{now: time.Now().UTC()}
env := newTestEnvWithClock(t, clk)
// ...
clk.Advance(httpadapter.SessionTTL - time.Hour)
```

So: make `TestOwnerSeesTheTwelveMonthTrend` anchor on real now instead of an absolute
date, and derive everything it asserts from that anchor.

Concretely, the three things currently hardcoded to July 2026 that must become
relative:

1. **The clock.** `time.Date(2026, 7, 28, …)` → an anchor derived from
   `time.Now().UTC()`. Anchor to a stable point *inside* the current month — the 28th
   of the current month is not safe (February), and the 1st risks timezone edge cases
   around month boundaries. Something like the 15th at midday UTC of the current month
   is stable in every month of every year.
2. **`openingBalanceAsOf: "2026-07-01"`** → the first day of the anchor's month,
   formatted `YYYY-MM-DD`. Note `usecase/account.go:177` rejects an opening balance
   dated more than a day in the future against `Clock.Now()`, so it must not drift past
   the anchor.
3. **The window assertions** — `points[0].Month != "2025-08" || points[11].Month != "2026-07"`
   → computed from the anchor: `points[11]` is the anchor's own month, `points[0]` is
   eleven months earlier. Format `YYYY-MM`.

Everything else the test asserts is about *shape and semantics*, not absolute dates —
twelve points, a gap month serialising `"netWorthMinor":null` rather than being
omitted, `complete` being pinned at byte level because a missing JSON key and `false`
decode to the same Go zero value, and `changeBasisPoints` being absent when the prior
month is unknown. **Keep all of it.** Those assertions are the point of the test and
several carry comments explaining defects they caught.

### Prove the fix is real

A green test is not enough here — the whole failure mode was a test that was green for
thirty days while being wrong. Do both:

```bash
export PATH="/Volumes/Oink_Machine/.local/opt/go-v1.24.2/bin:$PATH"
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
cd api && go test ./internal/adapter/http/ -run TestOwnerSeesTheTwelveMonthTrend -v
```

Then **mutation-check it**, per the repo's `proving-tests-can-fail` skill: change the
production window from twelve months to eleven and confirm the test goes red for the
right reason, then restore. A test that cannot fail proves nothing.

And state plainly in your report that the fix is verified only against today's date —
the whole class of bug is time-dependence, so say what you did and did not prove.

### Update the test's doc comment

It currently promises reproducibility it does not deliver. Say instead that the clock
is anchored to real now because session expiry is enforced by Postgres's `now()` and
cannot be moved by the injected clock, so an absolute past anchor rots one TTL after
it is written. That sentence is the whole lesson; leave it where the next person will
hit it.

### Record it

`docs/LEARNING.md` — this belongs in an existing pattern, not a new section. Read the
file first; the repo's rule is that a defect matching an existing pattern is added
there as evidence, "because the repetition is the point." Pattern 3 (a simulated
environment lied) and the "claims need checking against the code" pattern are both
plausible homes; the test's own comment asserting reproducibility it lacks is the
detail worth keeping.

`docs/FEATURE_TRACKER.md` needs nothing — no feature changed.

---

## 2 · Job two: M2 — the seven defects

`docs/superpowers/plans/2026-08-28-hearth-ui-polish.md`, **Tasks 7 through 14**. Each
task carries its files, its steps, and the test to write. Branch off `main` once M1
merges:

```bash
git checkout main && git pull && git checkout -b ui-defects
```

The seven, in the plan's order: the Transactions month contract (Task 7, the only Go
change), the achieved-goal card (8), inline validation (9), the ⌘K chip (10), the
month filter's opening value (11), modal focus (12), and `<1% used` (13). Task 14 is
the walk and docs.

### Task 7 is the one with a real decision in it

The plan already records the ruling, but know why: `parseTransactionFilter`
(`api/internal/adapter/http/transaction_handlers.go:228`) sets the summary's month
unconditionally but sets `filter.Month` only inside the `if` — so the default request
lists every month while summarising the current one. The screen reads "0 in August
2026" above ten July rows. The handler's own doc comment thirteen lines above states
the contract the code breaks.

The plan's decision is **not** to scope the list to the month (that would quietly
remove all-time browsing from a page titled *All transactions*) nor to widen the
summary (that breaks "Spent this month"), but to make the default explicit: the screen
opens on the current month with that month shown in the filter, and clearing it widens
list *and* summary together via `month=all`.

**One thing the plan flags and does not answer:** check what a zero `month` does to
`MonthSummary` before assuming the `month=all` early return is safe. Read
`api/internal/usecase/transaction.go`'s `MonthSummary` and decide there. Do not guess.

Also update `TransactionFilter.Month`'s doc comment in `usecase/ports.go` — it says
"Zero means every month", which stays true of the type but is no longer what the HTTP
layer sends by default.

This changes a documented route behaviour, so `docs/SYSTEM_DESIGN.md` gets updated in
the same change, via the `maintaining-system-design` skill.

### Rulings from M1 that carry into M2

- **`GoalCard.tsx` has no form fields.** An earlier census counted the substring
  `required` and scored it 5; those are all `requiredMonthlyMinor` /
  `requiredMonthlyOk`. It is not part of Task 9. The spec is corrected but do not be
  surprised by stale references.
- **Task 9's real shape:** the validation messages already exist and are already
  wired. `noValidate` appears in **zero** of the app's fifteen forms, so the browser
  intercepts submit before `handleSubmit` runs. `TransactionModal.tsx:236-241` says so
  in its own comment. The fix is one attribute plus per-field JS checks — not a new
  error system, and not react-hook-form (`formState` appears in zero files).
  **The class is all fifteen forms; the plan deliberately fixes one and names the
  rest as a follow-up.** Do not sweep them all in silently.
- **`.tabular` now has a stated rule**, in the `.tabular` comment in
  `web/src/index.css`. It belongs where figures stack or repeat; not on a lone
  display figure, not on a figure inside a prose sentence. If M2 adds money anywhere,
  apply the rule from that comment rather than from any list.
- **M3's Task 16 no longer adds `tabular` to `BreakdownCard`'s Net row** — M1 brought
  it forward. Task 16 still owns that row's weight and size.

---

## 3 · Things that will bite you

**`go` is not on `PATH`.** Nor is the Docker socket configured. Export all three
variables shown in §1 before any `go` command or any `make` target that shells out to
one. The Go suite needs testcontainers and takes ~4 minutes.

**Never pipe a gate into `tail`.** I reported `make test` as passing on exit code 0 —
that was `tail`'s exit code, not `make`'s, and the run had actually failed. Use
`set -o pipefail`, or read `${PIPESTATUS[0]}`, or don't pipe.

**Two browsers are available and they are not equivalent.** The chrome-devtools MCP
browser drives real CDP input; its hover works. The Claude-in-Chrome browser sets
`:hover` matching but **does not repaint CSS hover styles** — I nearly filed a false
bug against `GoalCard` before catching it with a control test on a nav hover I had
already proven working elsewhere. Use chrome-devtools for anything involving hover,
focus or computed style.

**There are two households on the dev database and each hides bugs the other shows.**
The seeded one (`andreas@hearth.family` / `hearth-dev-password`, from `make seed`) has
a populated ledger and several members but **no goals and no bills**. The other has one
account, one transaction and one *achieved* goal — which is the only way to reach the
empty-card defect in Task 8. Check which household you are signed into before
concluding a screen is fine.

**The repo means it about browser verification.** CLAUDE.md records that the product
owner asked for it explicitly on 2026-07-30, after a feature that verified "15 of 15"
still surprised them in first-run use. Every defect in M2 was found in a browser and
should be closed in one. During M1, driving the real page caught three things no
reviewer and no test could: a design token silently tree-shaken out of the build, a
hover state that erased a status cue, and one card rendering the same number twice
with its decimals 4.8px apart.

---

## 4 · One open question for the product owner

Not a defect, and not yours to decide silently: Schibsted Grotesk pads `,` and `.` to
a full digit advance under `tabular-nums`, so column amounts now read looser
(`−S$5 , 860 . 00`). The payoff is measured and real — every amount in the ledger puts
its decimal at the same x-coordinate across currencies, which right-alignment alone
never achieved. It is already removed from every lone figure where it cost width and
bought nothing. If the owner says the columns read too loose, it reverts in one commit.
It is raised in PR #10.
