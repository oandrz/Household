# UX repair (M1) — verification walkthrough

Run 2026-07-31 against the running dev stack at http://localhost:5173, branch
`fix/ux-repair` at commit `8fe1f1a`, in a real Chrome window at a
**2752 x 1010 CSS px** viewport. Two households were walked: a fresh one
created through self-serve sign-up, and the seeded one. Criteria from Task 7
of `2026-07-31-hearth-ux-repair.md`.

Every number below is a `getBoundingClientRect()` reading taken in the page
itself. None is an estimate, and none was carried over from a previous walk —
a measurement you did not take on the tree you are shipping is not evidence.

**Result: 19 of 20 criteria pass. Criterion 15 passes only after a fix the
walk itself found. Criterion 8 is deliberately unmet and is not M1's to
meet.** Read the notes at the end before quoting the headline: three of these
passes were reached by an interpreted rather than a literal path, and a
silent pass over an interpreted path is how a "15 of 15" walk still surprises
the product owner on day one (`docs/LEARNING.md` pattern 13).

## Fresh household

Signed out, signed up `alex@newhouse2.test`, collected the mail from Mailpit
at http://localhost:8025, completed the second screen as "Alvarez household" /
SGD / Alex.

| # | Criterion | Result |
|---|---|---|
| 1 | Sign-up step 1's button reads "Send me a set-up link" | **PASS** — the `button[type=submit]` accessible name is exactly that, and no "Create household" button remains on that screen. Step one only mails a link; naming it after what step two does was the promise the screen could not keep |
| 2 | Step 1's headline is the family copy | **PASS** — "One household for the whole family. Set it up once, invite your partner, add the kids later." |
| 3 | Step 1's footnotes | **PASS** — "You can invite your partner right after — nothing is shared until they accept." kept; "Your household data stays inside your household." |
| 4 | Currency select is grouped | **PASS** — `<optgroup label="Common">` holds 17 options, `<optgroup label="All currencies">` holds 116, for 133 currencies plus the placeholder = 134 `<option>` elements. 17 + 116 = 133 = every currency the server sent, so grouping dropped nothing |
| 5 | The placeholder is first and outside both groups | **PASS** — `select.children[0]` is `<option value="">Choose a currency</option>`, the only direct `<option>` child, and `select.value === ""` on load. A placeholder swept into a group would be selectable as a currency |
| 6 | Household-name helper says where the name appears | **PASS** — "Shown at the bottom of the sidebar, beside your name. Change it any time." |
| 7 | Sidebar after landing | **PASS** — Overview, MONEY (Finances, Transactions, Budget), Settings. No Marriage, no Family. Footer reads "Alvarez household / Free plan" |
| 8 | `/` is still the Overview placeholder | **DELIBERATELY UNMET** — it reads "Arriving in slice 5." M1 never claimed to fix it; `2026-07-31-hearth-interim-overview.md` is the plan that does. Recorded as unmet rather than passed, because the criterion as written is a check on the state of the page, not on M1's scope |
| 9 | Transactions with zero accounts offers the way out | **PASS** — the centre reads "Add an account first", the generic "Nothing logged yet." is absent, "+ Add transaction" is `disabled`, and an "Add an account" link to `/money` renders at x=1431, inside the content column. The disabled button now explains itself where the eye already is |
| 10 | That link works | **PASS** — lands on `/money`, showing the empty Finances state |
| 11 | Adding an account reverts the empty state | **PASS** — after DBS Everyday (cash, S$5,000, SGD), Finances shows net worth S$5,000.00; back on Transactions the centre reverts to "Nothing logged yet.", the account-first link is gone, and "+ Add transaction" is enabled |
| 12 | `/marriage` no longer names a slice | **PASS** — renders "Page not found." at pathname `/marriage`, shell-less: `/Finances/` is absent from the body, so no sidebar. `router.tsx`'s header comment records both consequences and `router.test.tsx` pins the fall-through |
| 13 | Settings still lists every space | **PASS** — the Spaces panel shows Money (Parents), Marriage (🔒 Parents only), Family (Everyone) and "+ New space (Kids, Home, Travel…)". Dropping a space from the *sidebar* must not drop it from *Settings*: the space still exists, it just has nowhere to go yet |
| 14 | Settings toggles sit beside the labels naming them | **PASS** — see the measurement table below |
| 15 | Sign-in screen copy describes the household the domain models | **PASS AFTER FIX** — the walk found "Sign in to pick up where you both left off." still live. Fixed in `8fe1f1a` to "Sign in to pick up where you left off." and re-verified in the same browser. See note 1 |

## Seeded household

`docker compose exec api go run ./cmd/adminctl seed`, signed in as
`andreas@hearth.family`: four members, real accounts, 10 transactions in July
2026, a budget. This is the pass the original critique never reached — the
bounded container measured against real data rather than an empty screen.

| # | Criterion | Result |
|---|---|---|
| 16 | Sidebar with real data | **PASS** — the same five destinations; footer "Andreas & Christine / Free plan" |
| 17 | `/money` populated, nothing clipped | **PASS** — content container 1204px wide, right edge at 2096; **0** elements inside `<main>` extend past that edge; `document.documentElement.scrollWidth === innerWidth` (2752) |
| 18 | `/money/transactions` populated | **PASS** — container 1204px, 0 overflowing elements, `scrollWidth` 2740 < 2752 (the vertical scrollbar, no horizontal page scroll); real rows render |
| 19 | `/money/budget` populated | **PASS** — container 1204px, 0 overflowing elements, no horizontal page scroll |
| 20 | Settings populated (4 members, 3 spaces) | **PASS** — see `settings-after.jpg` |

## The measurement that motivated the milestone

Same viewport, before and after Task 1's 1204px container. This is the defect
in numbers: a heading and the button that acts on it, two thirds of a screen
apart, with a green unit suite the whole time.

| element | before | after |
|---|---|---|
| `/money/transactions` "All transactions" heading, x | 170 | 928 |
| `/money/transactions` "+ Add transaction" button, x | 2577 | 1921 |
| gap between them | **2407px** | **993px** |
| `/settings` "Budget over-spend alerts" toggle, x | 2653 | 1997 |
| `/settings` "Weekly family digest (Sun 8am)" toggle, x | 2653 | 1997 |

## Screenshots

`docs/superpowers/plans/2026-07-31-hearth-ux-repair-screenshots/`

- `transactions-before.jpg` — md5 `eb8b4d2adf0a98240839948461a38e58`
- `transactions-after.jpg` — md5 `45918e21f831fc8cc2dab633386a8e48`
- `settings-after.jpg`

The two transactions shots differ, and the md5s are recorded so a later
reader can check that rather than take it on trust. This is the check
`docs/LEARNING.md` carries from the finance-fixes branch, where a "fixed"
layout produced two byte-identical screenshots and the fix had not actually
landed. Two identical files mean the change did not reach the browser.

## Gate

```
make test-web   — 28 files, 278 tests, all pass (at 151ae4d; 8fe1f1a kept the count)
make typecheck  — clean
make lint       — arch lint, tsc, eslint, go vet: clean
```

**`make test` was not part of this walk's evidence, and is recorded
separately.** The Go suite runs through testcontainers and needs a Docker
socket, which the walk did not have. Every one of M1's seven commits is under
`web/src`, so there is no Go change for it to cover — but "`make lint && make
test` green" is this project's stated bar, and this walk did not meet the
letter of it. The controller started the Go suite after the walk and records
its result on its own; nothing in this file claims a result nobody watched.
If you are reading this to decide whether M1 is safe to merge, the Go run is
the other half and it is not here.

## Notes rather than silent passes

1. **Criterion 15 is a fix, not a clean pass.** Task 4 swept the audience copy
   with a grep for `two owners`, `two of you` and `both of you`. The surviving
   string read "you both", which none of those three patterns match, and it
   was still live on the sign-in screen when the walk opened it. Fixed in
   `8fe1f1a` and re-verified. The lesson belongs to the grep, not to the
   decision: see `docs/LEARNING.md` pattern 1.
2. **Criterion 8 is deliberately unmet, not failed.** `/` still shows the
   Overview placeholder reading "Arriving in slice 5." M1 removed the
   placeholder *spaces* — Marriage and Family — from the navigation; Overview
   keeps its placeholder because it is the app's own landing page and cannot
   simply be dropped from the sidebar. `2026-07-31-hearth-interim-overview.md`
   is the plan that replaces it.
3. **Settings has an after screenshot but no before.** The brief asked for a
   before/after pair on both screens and only the Transactions pair exists.
   Settings' before-and-after is recorded as numbers instead — the two
   notification toggles moved from x=2653 to x=1997 — which is the same
   evidence in a form that cannot be confused with a re-render of the same
   state. Recorded as a gap in the evidence rather than left for a reader to
   notice the missing file.
4. **The seeded budget reads "805% used".** Those are the seed fixture's own
   figures (S$10,872.09 spent against S$1,350.00 budgeted), not a defect of
   this milestone. Noted so a later reader does not open a bug for it.
5. **The not-found page is bare, and that is an open product decision.**
   `/marriage` renders an unstyled "Page not found." with no sidebar and no
   link home. Because the fall-through sits on `rootRoute` rather than inside
   the shell, `RequireAuth` never runs: a signed-out visitor following an old
   bookmark gets that bare page instead of the sign-in screen.
   `router.tsx`'s header comment records both consequences and
   `router.test.tsx` pins the behaviour, so neither can be re-broken silently.
   Whether that page should gain a link home is a product decision, open, and
   deliberately not taken in M1.

## What the walk changed

One product defect, at criterion 15: a surviving "you both" on the sign-in
screen, fixed on the branch (`8fe1f1a`) and re-verified live before the walk
was called done. No test caught it and none could have — Task 4's own tests
assert the strings the task changed, and this was a string it never found.
The browser walk is what found it, which is the entire argument for doing
one.

The five LEARNING entries this milestone shipped (a placeholder as a live
navigation destination, an unbounded layout that no unit test could see, a
disabled control explaining itself where nobody looks, copy describing a
two-person product against a family domain, and the concept-grep that missed
a fourth phrasing of the same idea) are recorded in
`docs/LEARNING.md` under the patterns they are evidence for, not as a new
section each — the repetition is the point.
