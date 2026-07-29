# Transactions — definition-of-done walk

Walked 2026-07-29 against the `transactions` worktree, in a real browser
(Playwright driving Chromium) on a wiped database: `make down`, `docker volume
rm hearth_hearth-pgdata`, `make up && make seed`.

**Result: 15 of 15 criteria pass**, plus the three carried-forward items and
the modal open/trap/Escape/backdrop check. One defect was found and fixed —
a keyboard focus indicator missing on the ledger's Kind filter — and the
first attempt at that fix was itself caught half-wrong by the same walk (see
"What the fix's own near-miss showed", below).

Screenshots referenced below are in
`docs/superpowers/plans/2026-07-29-hearth-transactions-screenshots/`.

---

## Before the criteria: environment

Both Docker engines were checked before starting: colima and Docker Desktop
both showed zero running containers (`docker ps` on each), so the two-engine
trap that cost the Accounts walk an hour did not recur here.

A stale `hearth_hearth-pgdata` volume from earlier work in this worktree *did*
recur in a smaller form: the first `make down && make up` came up clean, but
`make seed` refused with "a credential-less user named Kayla already exists
with no membership in this household," because `make down` does not drop
volumes and this worktree's Postgres volume already held data from a previous
run. Removing the volume explicitly (`docker volume rm hearth_hearth-pgdata`)
and repeating `make down && make up && make seed` produced a clean seed. Not
new to `docs/LEARNING.md` — it is the same "a running/stale stack quietly
carries state past where you think you reset it" family as the two-engine
trap and the migration-skip item already recorded there — but worth naming so
the next walk in this worktree checks `docker volume ls` before concluding a
`make seed` refusal is a product defect.

---

## The criteria (written before walking, per the task brief)

1. Signed in as Andreas, `/money/transactions` loads and the sidebar reaches it.
2. The category dropdown is populated on a **fresh** household — sign up a new
   one and confirm the starter set appears without any transaction existing.
3. Logging an expense on DBS lowers that account's balance on Finances by
   exactly the amount, and lowers net worth by the same.
4. Logging income on DBS raises both by exactly the amount.
5. A transfer between two SGD accounts moves both balances and leaves **net
   worth unchanged to the cent**.
6. A transfer from DBS (SGD) to BCA (IDR) requires the amount received, and
   credits BCA with the rupiah figure typed — not a converted one.
7. A same-currency transfer with a smaller received amount (a fee) is accepted,
   and net worth falls by exactly the fee.
8. An expense dated before the account's opening balance is saved, marked on
   the row naming the account, leaves the balance unchanged, and **is
   included** in "Spent this month".
9. "Spent this month" counts expenses only — logging income and a transfer
   does not move it.
10. The five filters each narrow the list, and the account filter shows a
    transfer under **both** of its accounts.
11. With more than 50 transactions, "Load older transactions" appends without
    repeating or skipping a row; adding one mid-scroll does not break it.
12. Clicking a ledger row opens the modal **populated**; changing the amount
    and saving moves the account balance by the difference. Deleting from
    that same modal removes the row and restores the balance.
13. A limited member holding Money (invite one, give them Money in Settings)
    sees **no** Transactions link and gets a 403 on `/api/v1/transactions` —
    reads included.
14. The Finances page shows the five newest transactions and "See all" reaches
    the ledger.
15. A household with no accounts sees the add button disabled with the
    explanation, not an empty modal.

Three carried-forward items from the reviews, verified inline rather than as
a sixteenth item:

- A. The whitespace-only "Amount received" on a cross-currency transfer is
  reachable and produces "Enter what actually arrived." (not dead code).
- B. Re-clicking the already-active kind button does not discard a chosen
  category or overwrite a typed bank-fee amount.
- C. The Kind segmented control behaves as a native radio group: arrow-key
  cycling between All / Expense / Income, and a visible focus ring.

Also verified outside the numbered list: the modal genuinely **opens**, traps
focus, and closes on Escape and on backdrop click.

---

## Results

| # | Criterion | Result |
|---|---|---|
| 1 | `/money/transactions` loads; sidebar reaches it | **PASS** (interpreted — see below) |
| 2 | Category dropdown populated on a fresh household, zero transactions | **PASS** |
| 3 | Expense on DBS lowers balance and net worth by exactly the amount | **PASS** |
| 4 | Income on DBS raises both by exactly the amount | **PASS** |
| 5 | Same-currency transfer moves both balances, net worth unchanged to the cent | **PASS** |
| 6 | Cross-currency transfer requires amount received, credits the typed figure | **PASS** |
| 7 | Same-currency transfer with a smaller received amount (fee) accepted | **PASS** |
| 8 | Pre-opening expense marked, balance unchanged, included in month spend | **PASS** |
| 9 | "Spent this month" excludes income and transfers | **PASS** |
| 10 | All five filters narrow the list; account filter shows both sides of a transfer | **PASS** |
| 11 | 50+ transactions page correctly; mid-scroll insert doesn't break it | **PASS** |
| 12 | Row modal opens populated; edit moves balance by the diff; delete restores it | **PASS** |
| 13 | Limited member with Money: no Transactions link, 403 on reads | **PASS** (interpreted — see below) |
| 14 | Finances shows 5 newest transactions; "See all" reaches the ledger | **PASS** |
| 15 | No-accounts household: Add transaction disabled, with explanation | **PASS** |
| A | Whitespace-only "Amount received" reachable, produces its own error | **PASS** — disproves the "dead code" claim |
| B | Re-clicking the active kind button preserves category and amount | **PASS** |
| C | Kind filter: arrow-key cycling works; visible focus ring | **FAIL → FIXED, re-walked → PASS** |
| — | Modal opens as genuine `<dialog>`, traps focus, closes on Escape/backdrop | **PASS** |

### What the interesting ones actually showed

**Criterion 2 — a genuinely fresh household, not just a wiped one.** Rather
than reuse the wiped-and-reseeded Andreas & Christine household (which the
walk needed anyway for the numbered criteria and already had zero
transactions), this criterion was walked through the real self-serve
sign-up flow: `/sign-up` → magic link retrieved from Mailpit → household
setup form → `/money/transactions`. The category dropdown showed all 13
starter categories (Groceries, Dining out, Transport, Petrol, Household,
Kids & school, Health, Utilities, Insurance, Subscriptions, Fun & hobbies,
Giving, Income) with "Nothing logged yet" and "0 in July 2026" — the
starter set exists before any transaction does, seeded at household
creation rather than lazily on first use.

**Criteria 3–7 — the account-opening-date trap, and how it was worked
around.** `make seed`'s fresh accounts default their `Balance as of` to
today, and the balance sum only counts transactions dated *after* that date
(`occurred_on > opening_balance_as_of`, strict). A transaction logged the
same day as account creation therefore never moves the balance — which is
correct behaviour, but it meant the first attempt at criterion 3 landed
exactly on that boundary and produced a "before opening" row instead (useful
for criterion 8, not 3). Each account's `Balance as of` was moved to
2026-06-01 before criteria 3–7 were walked, so every transaction dated
"today" (2026-07-29) counted normally. Figures reconciled exactly at each
step:

- Criterion 3: DBS 8,195.05 → 8,095.05 (−100.00), net worth 8,597.95 →
  8,497.95 (−100.00) for a S$100 grocery/dining expense.
- Criterion 4: DBS 8,095.05 → 8,295.05 (+200.00), net worth 8,497.95 →
  8,697.95 (+200.00) for a S$200 income transaction.
- Criterion 5: a S$300 DBS→OCBC transfer moved DBS 8,295.05 → 7,995.05 and
  OCBC 1,000.00 → 1,300.00 while net worth stayed at **exactly** 9,697.95
  before and after.
- Criterion 6: a DBS(SGD)→BCA(IDR) transfer of S$500 with a *typed* received
  amount of Rp 6,000,000 (not the converted Rp 6,205,000 the static
  `{1, 12410}` rate would have produced) credited BCA with exactly
  Rp 6,000,000 — visible both in the ledger row ("−S$500.00 →
  +Rp 6,000,000") and in net worth's drop of S$16.52, which is exactly
  `500 − 6,000,000/12410`.
- Criterion 7: a same-currency DBS→OCBC transfer of S$200 with S$195
  received (a S$5 fee) was accepted and net worth fell by exactly S$5.00
  (9,681.43 → 9,676.43).

**Criterion 8 — re-walked with a dedicated account, per the advisor's
catch.** The first pass's evidence came from a state the walk itself later
destroyed (moving DBS's opening date to unblock criteria 3–7 erased the
"before opening" marker that had proven criterion 8). Re-walked cleanly with
a fourth account built for exactly this: "UOB One" (SGD, balance 1,000,
opening 2026-07-20), with an expense of S$60 dated 2026-07-10 — in July (so
it is inside the month the "Spent this month" figure covers) and before the
account's own opening date (so it is the case the criterion actually
names). The row read "Before UOB One's opening balance — it doesn't change
that balance." naming the account
(`criterion-8-pre-opening-expense-marked.png`); "Spent this month" rose from
S$77.24 to S$137.24, exactly +60.00; and UOB One's own balance on Finances
stayed at S$1,000.00, unmoved
(`criterion-8-uob-balance-unchanged.png`). All three parts of the criterion
now have a live witness that survives to the end of the walk, not one from
an earlier snapshot.

**Criterion 9 — the two negatives, not just one.** Logging the S$200 income
transaction (criterion 4) left "Spent this month" at S$145.50, unchanged
from before it; two same/cross-currency transfers afterward (criteria 5–7)
left it at S$145.50 still. Only the two genuine expenses (S$45.50 + S$100 =
S$145.50) ever moved it.

**Criterion 10 — all five, and the transfer-on-both-sides account filter
specifically.** Kind (radio group: All/Expense/Income), Account, Category,
Person and Month were each exercised. Filtering Account to "OCBC 360"
returned both transfers that touch it ("Transfer with fee" and "Move to
OCBC"); filtering to "BCA Tahapan" returned only the one transfer that
touches it. Filtering Category to "Groceries" narrowed to the one grocery
row. Filtering Person to "Andreas" (after setting that transaction's Paid by
via the edit modal) narrowed to it alone. Filtering Month to "2026-06"
correctly returned "0 in June 2026" even though 27 of the bulk transactions
created for criterion 11 were dated in June — proving the filter reads the
occurred-on date, not merely the presence of rows.

**Criterion 11 — 56 rows, keyset paging, and a reasoned negative result on
the mid-scroll insert.** 50 transactions were created via the authenticated
API (test-data setup, not the behaviour under test — the CSRF token was
read from the `csrf_token` cookie the same way `apiFetch` does) spread
across June–July, bringing the total to 55. The ledger's first page loaded
exactly 50 rows (the server's documented default limit); "Load older
transactions" appended the remaining 5 with no duplicate and no gap
(`Bulk txn 6` → `Bulk txn 1`, sequential). For the mid-scroll case, one
transaction dated 2026-06-15 was inserted *after* page 1 was already loaded
in the browser but *before* clicking "Load older" — that date falls inside
the range page 1 already covers (between "Bulk txn 6", 06-06, and rows
further up), so it correctly did **not** appear when "Load older" fetched
rows strictly older than the last-loaded cursor; that is the correct
behaviour for keyset pagination continuing from where it left off, not a
miss. A second insert dated 2026-05-15 — genuinely older than the last
loaded row — was added the same way and *did* appear, exactly once, as the
new oldest row, when "Load older" was clicked
(`criterion-11-pagination-56-rows.png`, 56 total rows, no duplicates).

**Criterion 12 — edit and delete, both against a real balance.** Editing
"Dinner out" from S$100.00 to S$130.00 moved DBS's balance by exactly the
S$30 difference (7,295.05 → 7,265.05). Deleting that same transaction from
the same modal (an in-page confirmation — "This transaction will be
permanently deleted. This can't be undone." with "Keep it" / "Yes, delete
it" — never a native `confirm()`) removed the row from the ledger and
restored DBS's balance by exactly the deleted amount (7,265.05 → 7,395.05,
+130.00).

**Criterion 13 — walked through both the CLI and the Settings path.**
Seeded children are credential-less by design (same as the Accounts walk's
own criterion 12), so a limited member was invited with real credentials:
`adminctl create-invite --email=... --name="Limited Tester" --role=limited
--capabilities=money`, accepted in a second tab. As that member: the
Finances page showed only "No accounts have been shared with you yet.",
with no "Recent transactions" section and no "See all" link at all (that
card only mounts in `FinancesPage`'s owner branch); navigating directly to
`/money/transactions` produced `Couldn't load your transactions.`, and both
`GET /api/v1/categories` and `GET /api/v1/transactions` answered `403` —
confirmed both via the browser's own console and a direct
`fetch('/api/v1/transactions', {credentials:'include'})` from the page,
which returned `{status: 403}`. The criterion's own wording — "invite one,
give them Money in Settings" — describes the owner toggling the capability
in Settings, which the invite-time `--capabilities=money` flag bypasses; to
close that gap rather than let it pass silently, Andreas then went to
Settings as owner and toggled the member's Money switch off and back on,
confirming the UI path grants and revokes the same capability the CLI flag
set at invite time.

**Criterion 14 — exactly five, and "See all" actually navigates.** With the
five original, same-day (2026-07-29) transactions in place and dozens of
older bulk ones, the Finances page's "Recent transactions" card showed
precisely those five, newest state per the design ("Groceries at NTUC",
"Freelance payment", "Transfer with fee", "Move to OCBC", "SGD to IDR
transfer"), and clicking "See all" navigated to `/money/transactions`.

**Criterion 15 — confirmed twice, once by construction and once
deliberately.** The freshly wiped-and-reseeded Andreas & Christine household
naturally started with zero accounts, showing "+ Add transaction"
disabled with "Add an account first, and transactions can attach to it." —
not an empty modal. The self-serve-signed-up household from criterion 2
showed the identical disabled state and copy independently.

**Carried item A — the whitespace-only "Amount received" is genuinely
reachable, disproving an earlier report's "dead code" claim.** On a
DBS(SGD)→BCA(IDR) transfer, typing three spaces into "Amount received" and
clicking Save transaction did **not** submit: the modal stayed open, and a
`role="alert"` read "Enter what actually arrived."
(`carried-A-whitespace-received-amount-error.png`). Native HTML `required`
validation only blocks a literally-empty value, so this proves the custom
JS-level check (a `.trim()` against the raw string) actually runs and
actually blocks the submit — not merely that the copy exists somewhere in
the source.

**Carried item B — holds in a real browser.** Opening the edit modal for
"Groceries at NTUC" (populated: amount 45.50, category Groceries) and
clicking the already-active "Expense" kind button left both the amount
(`45.50`) and the category (`Groceries`) exactly as they were — the
re-click did not discard the chosen category or reset the typed amount.

**Carried item C — a real defect, found, fixed, and re-walked twice.** See
the dedicated section below.

**The modal — genuinely opens, traps focus, closes on Escape and
backdrop.** `document.querySelector('dialog').matches(':modal')` was `true`
while the Add-transaction modal was open (a real `showModal()`, not the
declarative `open` attribute jsdom would tolerate). Pressing Tab 13 times in
a row (more than the 12 focusable elements inside) never moved focus outside
the dialog (`dialog.contains(document.activeElement)` stayed `true`
throughout). Escape removed the dialog from the DOM entirely. Clicking the
dialog element's own backdrop area (`document.elementFromPoint(5, 5)`
resolved to the `<dialog>` itself, styled `open:fixed open:inset-0
open:grid open:place-items-center open:bg-black/40` — the dialog *is* its
own backdrop, not relying on `::backdrop`) also closed it. This is exactly
what jsdom's `<dialog>` stub cannot exercise, and exactly why this task
exists.

---

## Carried item C, in full: a real accessibility defect, and its own near-miss

**What the walk found.** The Kind filter above the ledger (`All` / `Expense`
/ `Income`) is built as a real `<fieldset>` of three `<input type="radio">`s
so it is genuinely keyboard-reachable — but each radio carries `className="sr-only"`
(visually hidden, per the file's own comment, "so a screen-reader user does
what a real keyboard user does with a native radio group"). The pill a
sighted user actually sees is the `<label>` wrapping each radio, and that
label's className had no rule reacting to the radio's own focus state.

Confirmed by direct DOM inspection after a real `Tab` keypress (not a script
`.focus()` call, which browsers do not always treat as keyboard-triggered):
`document.activeElement.matches(':focus-visible')` was `true`, while
`getComputedStyle(label).outlineStyle` and `.boxShadow` were both `"none"`.
A screenshot confirmed it visually — the "All" pill looked identical focused
and unfocused (`carried-C-kind-radio-focused-no-ring.png`). Arrow-key
cycling itself worked correctly (`ArrowRight` moved both DOM focus and the
`checked` radio to "Expense", and the ledger correctly re-filtered) — only
the visible indicator was missing.

**Sibling hunt (`hunting-sibling-defects`).** The mistake is: a control's
real, focusable element is visually hidden, and the decorative element
standing in for it never reacts to that hidden element's focus state.
Grepped for the shape independently of the fix itself: `sr-only` appears
nowhere else in `web/src`; no `opacity-0`, `clip-path`, or `w-px h-px`
hiding pattern exists anywhere in the frontend; no `outline-none` or
`outline: none` appears anywhere in the CSS or TSX (so no *other* control
has had its native focus ring explicitly stripped, either); and
`TransactionFilters.tsx` is the only file in the frontend using
`type="radio"` or `type="checkbox"` directly — every other boolean control
in the product goes through the shared `ToggleSwitch` component, which is a
real, visible `<button>` and gets the browser's native focus outline for
free. Confirmed directly: Tabbing to the modal's own "Expense"/"Income"/
"Transfer" kind toggle (also real `<button>`s) showed a normal blue browser
focus outline with no CSS involved. No sibling found; the class is closed
by this one fix.

**The fix, and its own near-miss.** The first fix added
`has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-inset
has-[:focus-visible]:ring-accent` (Tailwind v4's `has-*` variant) to the
label. Re-walking confirmed a visible green ring on the unselected "All"
pill (`carried-C-kind-radio-focus-ring-fixed.png`) — but two screenshots of
the **selected**-and-focused "Expense" pill, taken before and after that
fix, came back **byte-for-byte identical**
(`carried-C-kind-radio-after-arrowright.png` and
`carried-C-kind-radio-arrowright-fixed.png`, same MD5). A dark-green ring
inset against the selected pill's own near-black (`bg-ink`) background has
effectively no contrast. The fix was revised to make the ring colour
conditional on the pill's own selection state — `ring-white` when selected
(against `bg-ink`), `ring-accent` when not (against the light `bg-card`) —
and re-verified with zoomed element screenshots of both states:
`carried-C-kind-zoomed.png` (a clear white ring on the selected "Expense"
pill) and `carried-C-kind-unselected-focused-zoomed.png` (a clear green ring
on the unselected "Income" pill).

**Test coverage, mutation-checked.** jsdom cannot render the ring or assert
its contrast — that half is what the browser walk exists to prove — but it
can pin the className that produces it, which is exactly what a future
className edit could silently drop. Added to
`web/src/features/money/TransactionsPage.test.tsx`:
`"gives the Kind radio group's label a focus-visible ring class, in a colour
that reads against its own background"`, asserting the selected pill's label
carries `has-[:focus-visible]:ring-white` and the unselected pill's carries
`has-[:focus-visible]:ring-accent`. Mutated twice and watched it fail both
times for the expected reason: reverting to a single `ring-accent` for both
states failed the assertion on the selected label's className; restoring the
two-colour version turned it, and the rest of the 14-test file, green again.

**Residual, named rather than hidden.** The final ring is a hard 2px inset
line; it was not checked against WCAG's 3:1 non-text-contrast ratio formally
(no contrast-checker tool was run), only eyeballed at 1x and via zoomed
screenshots against the two backgrounds it actually appears on
(`#1c1b18`-ish `bg-ink` and `#ffffff` `bg-card`). White-on-near-black and
accent-green-on-white both read clearly to the eye; a future palette change
to either background should re-check this pairing rather than assume the
conditional logic alone is sufficient forever.

---

## Interpreted criteria

Two criteria were met by a path other than their most literal reading. Both
are recorded here rather than passed silently, the same standard the
Accounts walk set for its own criterion 12.

**Criterion 1 — "the sidebar reaches it."** There is no direct
"Transactions" link in the main sidebar. `Sidebar.tsx`'s own comment says why:
"[the design's sidebar sub-page links]... belong to slices 2-4, which
haven't been built yet. Until they exist, one space has exactly one
destination" — Money is one clickable nav item to `/money` (Finances), and
Finances is where "See all" (criterion 14) reaches Transactions. The
criterion is met by that chain — sidebar → Money → Finances → See all — not
by a direct sidebar sub-item, and that is the design's own documented
scoping, not a gap in this feature.

**Criterion 13 — "give them Money in Settings."** The limited member's
Money capability was granted at invite time via `adminctl create-invite
--capabilities=money`, not by an owner toggling it afterward in Settings —
credential-less seeded children make "sign in as Kayla" impossible, and
issuing a real invite (as the Accounts walk's own criterion 12 also had to)
was the fastest way to a member with real credentials. To close the literal
"in Settings" gap rather than leave it, Andreas subsequently toggled that
member's Money switch off and back on in Settings, confirming the UI path
the criterion names actually grants and revokes the capability, in addition
to the CLI path already exercised for the 403 checks themselves.

---

## The gate

`make lint && make test` run at the end of this walk (see the task report
for the full output) — both green on the tree being integrated, including
the new focus-ring test and its mutation check.
