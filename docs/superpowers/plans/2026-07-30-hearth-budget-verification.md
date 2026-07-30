# Budget — verification walkthrough

Run 2026-07-31 (server clock 2026-07-30 UTC — the one-day skew shows up in
"days left", noted at criterion 5), from a wiped database (`compose down`,
`docker volume rm hearth_hearth-pgdata`, `up`, `seed`), in a real Chrome via
browser automation, against http://localhost:5173. Criteria from Task 17 of
`2026-07-30-hearth-budget.md`.

**Result: 15 of 15 pass — after two real defects the walk itself found at
criterion 9 were fixed, reviewed and re-verified live mid-walk.** Several
criteria needed their written arithmetic corrected against the shipped spec;
each correction is recorded at the criterion, pattern-13 style, in the walk
record below.

**Environment note, before anything else:** the walk lost its first half hour
to the two-engine trap `docs/LEARNING.md` already documents — recursed in a
new form. Colima auto-started mid-session and silently took over the default
docker context AND both socket paths the CLI resolves, while Docker Desktop's
seven-hour-old hearth stack kept the host ports. Two successive "wipe the
database" runs wiped a freshly-created colima stack the browser never talked
to, while the browser kept its session against Desktop's untouched database —
which looked exactly like "sessions survive a DB wipe" until `docker info`
on both sockets returned the same engine ID and `docker --context
desktop-linux ps` found the real stack. Every stack command for the walk ran
with an explicit `--context desktop-linux` from then on. Added as evidence to
the LEARNING two-engine entry.

| # | Criterion | Result |
|---|---|---|
| 1 | Sidebar MONEY group shows Finances, Transactions, Budget; Budget's link colours only on its own route | PASS |
| 2 | Unbudgeted month shows the design's empty state; no "Import last month" card when the previous month has no budget | PASS — July empty state, June unbudgeted so no Import card |
| 3 | "Create your first budget" opens the modal blank | PASS, with a script correction: the criterion said "Allocated S$0.00 visible, Left-to-allocate hidden" but the spec's modal section hides Allocated and Left-to-allocate **as a pair** while income is blank — which is what ships and what Task 14's tests pin. The script's phrasing contradicted the spec it derives from |
| 4 | Family-of-four template prefills the modal (design's ten caps on real categories); closing without saving leaves the empty state | PASS |
| 5 | Budget saved: income 9,100.00, caps Food-née-Groceries 800.00 + Dining out 450.00; page figures match pre-recorded spends | PASS — seeded July has zero transactions (accounts and transactions arrive by hand), so pre-save spends were recorded as S$0.00 and the saved page read Budgeted 1,250.00 / Spent 0.00 / Remaining 1,250.00 / pace 625.00 (= 1,250 ÷ 2 days left — server-UTC today 30 July, hence 2) |
| 6 | New S$60.00 Dining out expense moves Dining and Spent by exactly 60.00; pushing past the cap raises the over state | PASS — needed an account first (S$5,000 DBS created; seed has none). After 60.00: Spent 60.00, Dining 60.00/450.00, 5% (= round 4.8), pace 595 (= 1,190 ÷ 2). After a second 400.00 expense: Dining "460.00 / 450.00 · over" in the over colour, OverCount copy "Dining out is the only category over.", by-person rows Ethan 400 / Andreas 60 |
| 7 | Percent, Remaining, Daily pace agree with the pinned formulas by hand | PASS — 460/1,250 = 36.8 → 37%; remaining 790.00; pace 395 = 790 ÷ 2, floor |
| 8 | Edit: rename Groceries→Food inline, add "Arisan" 100.00, archive Petrol; grid updates; transaction dropdown loses Petrol; old Petrol rows keep their label | PASS — archive reached by adding a Petrol row (cap 0.00) and queueing its archive; the queued state showed "(archived)" with an Unarchive control before save. Dropdown source: Petrol absent, Food and Arisan present, `archived: true` on Petrol via `?includeArchived=true`. No Petrol transactions existed, so the keeps-label clause was vacuously met (recorded, not claimed) |
| 9 | Duplicate category name → inline 409-shaped error, modal open, nothing half-saved | PASS **after two defects found here were fixed** (commit `2538ac8`, task-scoped review + live re-verification): (a) reopening the modal rendered the archived Petrol line as "Unknown category" — the modal's name resolution used the archived-excluding list; now archived-inclusive, add-dropdown still archived-excluding; (b) the duplicate-name Add was a silent no-op — now an inline `"Food" is already a category name in this household.` with the input kept and no network call. Nothing was ever half-saved (verified server-side both before and after the fix). Note: the exact-duplicate path is guarded client-side, so the server 409 stays the net for races and case-variant collisions |
| 10 | Month picker: June empty (header named, no fake zeros), July intact on return, August offers "Import last month — Copy July's caps" | PASS — June also correctly shows no Import card (its previous month, May, is unbudgeted) |
| 11 | Import July into August, save; History modal correct | PASS, with a script correction: the criterion predicted "History lists August 'so far'" — wrong against the spec's own pinned semantics (History anchors on *today* and walks closed months back; a future month never appears). Actual, correct: July "so far" with the asterisk footnote, summary cards "—", "—", "0 of 0" (no closed budgeted months — the zero-closed branch renders properly), no June/May zero rows, no Export CSV control anywhere. August's own page: caps present, Spent 0, and no pace card at all (future-month gating live) |
| 12 | History row click closes the modal and switches the page's month | PASS — July row → July page |
| 13 | IDR expense counts converted to the cent; a no-rate currency is excluded with a count | PASS — Rp150,000 Food from a new IDR account appeared as S$12.09 (= 150,000 ÷ 12,410, half-up at the cent), Spent 472.09, 35% (= round 34.97), remaining 877.91; a €20.00 expense from a new EUR account (no rate) left Spent unmoved and rendered "1 transaction is not counted: no exchange rate." IDR's zero-decimal input rule was enforced by the account form ("IDR doesn't use cents") |
| 14 | A limited member granted `money` is refused Budget reads | PASS — Kayla's Money toggle exercised in Settings; a credentialed limited member (Maya, via `adminctl create-invite --capabilities=money`, accepted in the signed-out browser) got wire `403` on `GET /budgets/2026-07` and an error line with no figures. Script correction: "the sidebar never offered the link" is wrong — the sidebar is capability-scoped, so Maya sees the Money group including Budget, exactly as the Transactions slice shipped it; the server refusal is the guard, the link is not. Marriage and Settings correctly absent for her |
| 15 | `PUT` without CSRF → 403; malformed month → 400 | PASS — `403 {"code":"CSRF_INVALID"}` with a session cookie and no token; `GET /budgets/2026-7` → `400 {"code":"INVALID_MONTH","message":"That month could not be read. Use YYYY-MM."}` |

## Gate

```
make lint   — arch lint, tsc, eslint, go vet: clean
make test   — Go suite ok across all packages; frontend 274 tests, 28 files: all pass
```

## What the walk changed

Two product defects, both at criterion 9, both fixed on the branch with tests
before the walk continued (`2538ac8`): the archived-category name resolution
in the reopened modal, and the silent duplicate-name refusal. Both were
invisible to the unit suites for the same reason: every modal test built its
category fixtures fresh and never reopened the modal against a
previously-saved budget, and the duplicate-name guard's *silence* passed a
test that only asserted nothing was created. The LEARNING entries shipped
with the fix.

Walk-script corrections (criteria 3, 11, 14 and the criterion-8 archive
path) are recorded at their rows — the walk-arithmetic lesson in LEARNING
pattern 13 now has four more data points, all caught in-walk rather than
after.
