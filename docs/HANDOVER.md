# Hearth — handover

Written 2026-07-27, after slices 0 and 1 shipped, updated the same day once
self-serve sign-up's code was done too, and again on 2026-07-30 when that
slice's browser walk finally ran (§1). This
is the document to read before picking the work back up, whether that is you
in three months or someone new.

---

## 1. Where things stand

> **Hearth is in production, since 2026-08-15.**
> <https://oink.mywire.org> — one Hetzner CX23 in Falkenstein, `5.75.239.188`,
> running the Compose stack behind Caddy. A real household exists on it and was
> created through the real sign-up form.
>
> **Ten of the twelve verification criteria pass**, recorded criterion by
> criterion in
> `docs/superpowers/plans/2026-08-10-hearth-production-verification.md`. Nothing
> in the product broke during the walk. Criterion 3 is deferred by
> [ADR 3](adr/0003-mail-stays-on-the-box.md) (no mail leaves the box), criterion
> 7 is half done (magic-link recovery proven, the lockout half not run), and
> criterion 8 (`adminctl unlock-household` against the live database) is simply
> unrun.
>
> **Backups run nightly to Cloudflare R2**, `age`-encrypted, since 2026-08-15.
> The whole loop is proven with real values — a backup was pulled back out of
> R2, decrypted and restored, reproducing all eleven tables and every monetary
> figure exactly. The private key is **not on the box**, so a box compromise
> yields ciphertext.
>
> **The escrow is real and tested, since 2026-08-15.** A printed recovery page
> is held outside this machine. It was proven by typing the key off the paper
> into a fresh file and restoring from that alone — all eleven tables and every
> monetary value matched live production. **The household now survives losing
> the box and losing the owner.**
>
> Deploying is `deploy/deploy.sh <git-sha>` on the box, with `--current` and
> `--rollback`. CI builds SHA-tagged images on every push to `main`; the box
> never updates itself, deliberately. `deploy/README.md` is the runbook.
>
> Two things measured on the box rather than assumed: a reboot recovers
> unattended in ~26 s with data, session and certificate intact; and the per-IP
> sign-up limiter genuinely keys on the real client (laptop `429` while a phone
> on mobile data was still accepted, with both real addresses in nginx's log
> rather than Caddy's `172.28.x.x`).

Everything shipped so far is walked end to end in a browser, Bills included
— its own walk ran 2026-08-10 and passed 15 of 15 (§4). Self-serve
sign-up's own 15-criterion walk ran on 2026-07-30 — three days after its code
was finished and reviewed — and **passed 15 of 15**, recorded in
`docs/superpowers/plans/2026-07-27-hearth-signup-verification.md`. No product
defect came out of it. Three criteria carry notes rather than silent passes:
the rate-limit criterion needed the API restarted first (the per-IP limiter is
in-memory and the walk's own earlier submissions had spent two of the five
requests), the private-window invite acceptance was met by signing out in the
same browser instead, and "four members" in Andreas's household reads as the
seeded state — three accepted members plus Christine's deliberately pending
invite.
Accounts (slice 2's first feature) closed that gap for itself: its walk ran
and passed, 15 of 15. Transactions (slice 2's second feature) is code-complete
and reviewed the same way, seventeen tasks deep with every task's own review
clean including fix rounds — and its own browser walk (Task 19, fifteen
criteria) has now run too. **Result: 15 of 15 pass**, recorded in
`docs/superpowers/plans/2026-07-29-hearth-transactions-verification.md`. One
real defect surfaced: the ledger's Kind filter (All/Expense/Income) hid its
real, keyboard-focusable `<input type="radio">`s with `sr-only`, and the
visible pill standing in for each one never reacted to the hidden input's
own focus state — Tab and arrow-key navigation moved real focus with no
visible indicator at all, catchable only in a real browser (`fireEvent.click`,
what every existing test used, never presses a key). Fixed in
`web/src/features/money/TransactionFilters.tsx`, and the fix's own first
attempt was itself caught half-wrong by the same walk: a single ring colour
was invisible against the selected pill's near-black background (two
screenshots, before and after, came back byte-identical), so the ring colour
had to become conditional on which of the two pill backgrounds it sits
against. Pinned by a new, mutation-checked test in
`TransactionsPage.test.tsx`. Two criteria were met by an interpreted rather
than fully literal path — the sidebar reached Transactions via Money →
Finances → "See all" rather than a direct sub-link at the time (the
finance-fixes branch has since added the design's grouped sidebar, giving
Money its own Finances and Transactions links directly), and the
limited-member capability was granted via
`adminctl create-invite --capabilities=money` before being additionally
exercised through the Settings toggle Andreas would actually use — both
recorded in the verification file rather than passed over quietly.

A final whole-branch review then found five more, one of them Critical and
now fixed: making `AccountView.Balance` a real sum changed what that value
*means*, and `AccountModal` — a file no task on the branch owned — went on
prefilling its Balance input from it and writing the result back as the
*opening* balance, so editing an account's currency silently restated
today's figure as the opening one and moved the household's net worth. The
wire now carries `openingBalance` alongside `balance` (both redacted for a
limited member), the form reads and is labelled "Starting balance", and it
shows the current balance read-only beside it. The other four were a port
doc comment describing the pre-Transactions world, two missing
balance-invariant tests the spec had named (a transfer leaves the pair's
total unchanged; a transfer straddling one account's opening date moves
exactly one balance and reports the two flags independently), two comments
asserting a Postgres row-comparison behaviour Postgres does not have, and no
test driving the three transactions write routes without a CSRF token.
`docs/LEARNING.md` pattern 1 carries the Critical one in full.

| Slice | Contents | State |
|---|---|---|
| 0 — Skeleton | Clean-architecture layout, Docker, Compose, Make, migrations, health endpoints | **Done** |
| 1 — Household & identity | Sign-in, magic link, invite acceptance, lockout, members, roles, capabilities, spaces, Settings | **Done** |
| — Self-serve sign-up | Sign-up, household provisioning, an ISO 4217 currency allowlist and list endpoint, `adminctl prune`, a per-IP rate limiter | **Done, browser walk 15/15** (2026-07-30) |
| 2 — Money | **Accounts**: manual entry, net worth, assets/liabilities breakdown, archive and restore — **done, browser walk 15/15**. **Transactions**: ledger, categories, filters, keyset paging, month-to-date spend — **done, browser walk 15/15**. **Budget**: envelope per category with pace, empty state and templates, Edit-budget modal with category create/rename/archive, History modal and month picker — **done, browser walk 15/15** (2026-07-31, two defects found at criterion 9 and fixed mid-walk; see `docs/superpowers/plans/2026-07-30-hearth-budget-verification.md`). **Goals**: savings targets whose progress is a contributions ledger (not an account balance), the New/Edit goal modal, contributions add/delete/list by source, the Monthly contributions card, and Budget's own manual rollover into a goal — **done, browser walk 15/15** (2026-08-01, one defect found at criterion 12 and fixed mid-walk: archive and restore shipped with no way to *archive* — every layer existed and no screen called `useGoals.archiveGoal`, so "Show archived" and every Restore button led out of a state no household could enter, `82453ff`; see `docs/superpowers/plans/2026-08-01-hearth-goals-verification.md` and `docs/LEARNING.md` pattern 15). **Bills**: recurring bills on a one-off/monthly/quarterly/yearly cadence, Mark paid writing a real expense transaction and its undo reversing all three writes, archive and restore, a subscriptions rollup — **done, browser walk 15/15** (2026-08-10, one defect found at criterion 14 and swept for the class rather than fixed as one instance: `BillsPage.tsx` answered a limited member's routine "not the owner" 403 with the same red alert a genuine server failure gets, where sibling `GoalsPage.tsx` already distinguishes the two — and the identical gap turned out to be sitting in `BudgetPage.tsx` and `TransactionsPage.tsx` too, fixed the same walk; see `docs/superpowers/plans/2026-08-09-hearth-bills-verification.md` and `docs/LEARNING.md` pattern 1). Accounts, Transactions, Budget, Goals, Bills all built and walked; **slice 2 (Money) is done** | **Done** |
| 3 — Marriage | Retros, Vision, Agreements | **Retros**: code-complete and reviewed clean across fifteen code tasks, `docs/FEATURE_TRACKER.md`'s Marriage rows updated — **its own browser walk has run and passed, 15 of 15** (2026-08-18, Task 17), recorded in `docs/superpowers/plans/2026-08-16-hearth-retros-verification.md`. **Vision**: real end to end — `VisionPage.tsx` (task 11 of a 15-task plan) renders the theme, pillars and milestones `useVision` exposes, and `VisionModal.tsx` (task 12) is the whole-document editor all three of its entry points now open (the header's Edit vision button, "+ Add milestone" and the empty state's own call to action), so a household can set a year's vision, not only see it. **Vision's own browser walk (task 15) has run and passed, 15 of 15** (2026-08-29), recorded in `docs/superpowers/plans/2026-08-28-hearth-vision-verification.md`; no product defect needed a code fix, and one latent defect the final whole-branch review re-classified out of "accepted trade-off" is recorded rather than fixed here — editing a vision whose linked goal was deleted resets that measure to a typed `0 of 1`, dormant only because Goals has no `DELETE` route to reach it through the product, so the fix belongs to whoever builds that route. Merged as `6c48f6f`. **Agreements: not started — Marriage's last feature, and the next work.** |
| 4 — Family | Calendar | Not started — its own dependency on Bills (bill dates on the month grid) is now satisfied |
| 5 — Overview | Read-only aggregation across 2–4 | **Interim page built** (2026-08-01, grown 2026-08-10, 2026-08-16) — `/` carries five of the design's **seven** cards (counted off `design/Household Dashboard.dc.html`'s own Overview screen — the money row of four, Marriage's "Next retro", "This week" and "Vision 2026"; its header "+ Add" is a button, not a card) that Money and Marriage can supply (net worth, this month's budget, goals on track, the next bill due, and — added by Retros — the next retro with its open action count), a setup checklist and a four-entry "+ Add" (Transaction, Account, Savings goal, Bill). The M2 walk on the first two cards and the two-entry menu ran **14 of 14**, one real defect found and fixed mid-walk; the goals card and its menu entry were covered by Goals' own walk at its criterion 11 (2026-08-01); the next-bill card by Bills' own Task 18 walk. **The next-retro card is new since and has now been checked for overflow, not for content** — Retros' own Task 17 walk (2026-08-18, 15/15) verified it at all four widths (criterion 15: 305/305, 360/360, 768/768, 1440/1440, no overflow), but unlike Goals' criterion 11 and Bills' Task 18 walk, no criterion in Task 17 exercised the card's own figures — `openActionCount` was already pinned by its own fix and test beforehand, not by this walk. **Vision's own two surfaces shipped 2026-08-29** (Vision task 13): the `Vision 2026` card (`VisionCard.tsx`, one line per pillar showing that pillar's first measure with live figures) and the Vision check-in strip inside `NextRetroCard.tsx`, both covered by Vision's own 15/15 walk — so `/` now carries **six** of the seven, not five. The one remaining card ("This week" agenda) needs Family; the page grows into the designed one rather than being replaced |

Self-serve sign-up carries no slice number on purpose: it was specified and
built between slices 1 and 2, ahead of Money (see "What to do next" below for
why), and the rest of the roadmap keeps its original numbers so every existing
reference to "slice 2 (Money)" elsewhere still points at the right thing.

The definition of done for slices 0 and 1 was walked in a real browser on a
wiped database: **10 of 10 criteria pass**. The record, including the three that
failed on the first walk and what was done about them, is in
`docs/superpowers/plans/2026-07-26-hearth-identity-verification.md`.

Self-serve sign-up's own definition of done is a 15-criterion walk, written
down in `docs/superpowers/plans/2026-07-27-hearth-self-serve-signup.md`
(Task 32) — a stranger creating a household, the endpoint's silence holding
across the first five rapid sign-ups and the per-IP limit correctly answering
`429` on the sixth, `adminctl
unlock-household --email` resolving the right household, and `adminctl prune`
refusing a window under seven days among them. **Result: 15 of 15 pass**, run
2026-07-30 from a wiped database and recorded in
`docs/superpowers/plans/2026-07-27-hearth-signup-verification.md` — including
the notes on the three criteria that needed interpreting (the in-memory
limiter restart, the sign-out-instead-of-private-window invite acceptance,
and "four members" meaning the seeded three plus Christine's pending invite).

Accounts' own definition of done is a 15-criterion walk, written down in
`docs/superpowers/plans/2026-07-28-hearth-accounts.md` (Task 41). **Result: 15
of 15 pass**, recorded in
`docs/superpowers/plans/2026-07-28-hearth-accounts-verification.md` — the sign
rule end to end (a loan reads positive in the accounts list and negative in
net worth), convert-then-add against a real database (an IDR account raised
net worth by the exchange rate's cent-accurate figure), the redaction at the
wire (a limited member's response carries no `balance` key, no `balanceAsOf`,
no `summary` at all), and the primary-currency-change state (switching to EUR
blanks the net worth figure with a named explanation, never a zero) among
them. The walk's own script had one defect: criterion 12 said "sign in as
Kayla in a private window", which is not executable — seeded children are
credential-less by design — so it was met instead by inviting a limited
member with real credentials, which is the more realistic path anyway. Fixed
in the record, not in the product.

The walk itself lost most of its first hour to something unrelated to the
feature: this machine runs two Docker engines, and a five-hour-old Docker
Desktop stack was silently holding the host ports colima's stack needed. See
`docs/LEARNING.md` for the lesson that cost the hour.

Transactions' own definition of done is also a 15-criterion walk, written down
in `docs/superpowers/plans/2026-07-29-hearth-transactions.md` (Task 19) — a
fresh household's category dropdown populated before any transaction exists,
the sign rule and the currency-mismatch rule both proven against a real
database (a cross-currency transfer credits the destination in the figure
actually typed, not a converted one), a transaction dated before its account's
opening balance saved and marked but still counted in "Spent this month," the
five filters each narrowing the ledger with the account filter matching a
transfer on both sides, keyset paging surviving a row inserted mid-scroll, and
a limited member holding Money refused the ledger itself (reads included, not
only writes) among them. **Result: 15 of 15 pass**, recorded in
`docs/superpowers/plans/2026-07-29-hearth-transactions-verification.md`. All
seven money-movement criteria (expense, income, same-currency transfer,
cross-currency transfer, a same-currency transfer's fee) reconciled to the
cent against a real database, including the account-opening-date boundary
that Task 9's designated mutation (`>` to `>=`) protects. One real defect
came out of the walk rather than a false claim from a stub: the Kind
filter's radios are `sr-only` and their visible label never reacted to the
hidden input's own focus state, so keyboard navigation moved real focus
with no visible sign of it at all — a defect no unit test using
`fireEvent.click` could ever have pressed a key to find. Fixed, and the
fix's own first version was itself caught half-wrong by the same walk
before-and-after screenshots (a single ring colour disappeared against the
selected pill's dark background) — pattern 3 of `docs/LEARNING.md` records
both. Two criteria were met by an interpreted path rather than a fully
literal one and are recorded as such in the verification file, the same
standard the Accounts walk set for its own criterion 12: the sidebar
reached Transactions through Money → Finances → "See all" rather than a
direct sub-link at the time, by the design's own documented single-nav-item
scoping — the finance-fixes branch has since built the grouped sidebar, so
Money now expands directly to Finances and Transactions links; and the
limited member's Money capability was granted at invite time via
`adminctl` before also being exercised through the Settings toggle the
criterion's wording names.

**The interim Overview (M2) shipped 2026-08-01**, after the UX-repair round
(M1) that preceded it. It added no endpoint, no table and no port — it is
composition over what Accounts, Transactions and Budget already expose — and
two extractions that stopped it being a fourth copy of one query and a second
copy of another. Its walk covered three member states because the seeded owner
exercises only one of them, and that is what caught the milestone's one real
defect: **a limited member holding `money` saw a page containing the word
"Overview" and nothing else.** Every unit test passed against it, before and
after, because each test covering that member asserted the *absence* of
something, and absence holds perfectly over a blank page. Recorded in
`docs/superpowers/plans/2026-07-31-hearth-interim-overview-verification.md`
and in `docs/LEARNING.md` pattern 2, which also carries the second finding
from the same milestone: the plan's own designated mutation could never go
red, because two guards defend that behaviour and removing either alone
changes nothing observable.

**Bills, Money's fifth and last feature, is code-complete, reviewed and now
walked — sixteen tasks deep, every task's own review clean including fix
rounds, plus Task 18's own 15-criterion browser walk (2026-08-10, 15 of 15
pass).** Two tasks landed real,
implementer-found defects along the way rather than review catching them
cold: Task 12's "All caught up" state could render directly above a bill
still overdue from a *prior* month, because such a bill contributes to
neither of the two totals the empty-state check compares — found and fixed
before commit, then verified live against the real backend before and after
(`17b2f78`); and Task 14's undo-refusal message ("only the most recent
payment, due X") could name a payment that no longer existed, with a sibling
staleness hole in Mark paid that review caught once the first was fixed —
another instance of "fix the class, not the instance" (§6 below), the
pattern this feature's own ledger names at least twice more on top of it.
Review also found and fixed three defects with no working state to
demonstrate them from: `SetBillNextDue` committing two of three writes
silently on a zero-row match (Task 5), a bill silently re-pointed at an
archived account with no way back to pay it (Task 9, carried into Task 10),
and `MarkPaid` answering a bare 403 for three different refusal reasons
where the design's own error table names two of them (Task 10). By the
ledger's own running tally, six separate prescribed mutations or proof
techniques across this feature's plan turned out to prove nothing, or the
wrong thing, and every one was caught before the task closed — see
`docs/LEARNING.md`'s Database-and-repositories catalogue (the pgx
transaction-leak instance) and Pattern 2 (the recurring NOT_FOUND-message
one) for two of the six in full.

**Task 18's own walk (2026-08-10) then found a seventh — and, once the sweep
it prompted ran, two more beyond that — of the shape "code-complete and
reviewed" cannot catch by construction: a limited member holding
`money` who visited `/money/bills` (the sidebar's own link, reachable because
the route guard checks only the capability, never the role) saw the same red
"Couldn't load your bills" alert a genuine server failure produces, for the
routine, expected 403 the money-AND-owner guard answers with. `GoalsPage.tsx`
carries the identical guard and already distinguishes the two states with its
own `goals-owner-only` explanation; `BillsPage.tsx` had not been given the
same branch, and no test — not even an absence test — covered either shape of
its `bills.error` case. Fixing it and re-reading `router.go`'s own comment —
which names transactions, categories, budgets, goals **and** bills as one
money-AND-owner group, not Bills alone — found the identical gap already
sitting in `BudgetPage.tsx` (one line worse: `budget.error || !budget.data`
collapsed the 403 into the same branch as the ordinary type-narrowing guard)
and `TransactionsPage.tsx` (no branch and no test either). All three fixed
to mirror `GoalsPage.tsx` exactly, each with its own copy and its own
mutation-checked test pair (`docs/LEARNING.md` pattern 1 — Goals fixed the
instance, and fixing Bills' instance is what prompted the sweep that found
the other two, rather than the task closing on Bills alone). Full record,
all fifteen criteria, the three-page sweep, and the answers to the walk's
three product questions (Paid-this-month buckets by `due_on` so a paid
prior-month bill is invisible there though the money is real on the ledger;
Spending by person is invisible for any month with no budget row, bills or
not; a no-rate subscription renders its row but not its total, with the page
footnote as the only explanation) are in
`docs/superpowers/plans/2026-08-09-hearth-bills-verification.md`.

Two screens the design marks "· not built" are deliberately absent: the **kids
view** and **custom space pages**. That is the design's own scoping, not an
omission.

**Every screen now works on a phone, as of 2026-08-16.** Before that round the
app was not merely cramped on mobile but unusable: `AppShell`'s fixed 236px
sidebar left `<main>` 124px wide at a 375px viewport, and `/sign-in` scrolled
sideways on a card with no max-width. Nothing was redesigned — the product
owner's constraint was "same UI, layout and structure" — so the sidebar became
an off-canvas drawer below `lg` (1024px) and everything else reflows at `sm`
(640px). Walked at 320/375/414/768/1024/1440 across every authenticated screen,
sign-in, sign-up and one modal from each family, with zero horizontal overflow
at any width; 1440 still measures 236 + 1204 exactly. The design file has no
mobile artwork to build against — it is a 1440px canvas with zero media
queries — so every mobile layout is invention, named as such in
`docs/superpowers/specs/2026-08-15-hearth-mobile-responsive-design.md`. Known
gaps are in §5 below, and the feature tracker's row is 🟡, not ✅, because of
them.

**The UI-polish round, 2026-08-28.** Three milestones off
`docs/superpowers/plans/2026-08-28-hearth-ui-polish.md`. **M1 merged** (PR #10,
`d83ff22`) — focus rings, the transition token, hover and active states, the
skip link, tabular figures, three unused font families dropped. **M2 merged** (PR #11,
`fa38c58`, branch `ui-defects`): the Transactions month contract
(the only Go change), the achieved-goal card, Hearth's own validation messages,
the ⌘K chip, the month filter's opening value, modal focus, and "<1% used" —
plus the fix that turns `main` green again, since
`TestOwnerSeesTheTwelveMonthTrend` had been failing daily on a clock pinned to
an absolute past date. **M3 merged** (PR #12, `425c7a7`, branch `ui-consistency`)
(Tasks 15-18): Bills' "All caught up" and Budget's insight panel get the border
their siblings have, Finances weights Net as the total it is, and the Retros
detail panel gets a composed empty state. **The plan is finished with M3** —
there is no M4 in it.

Two of M3's decisions are worth knowing before touching those files. The
caught-up and insight panels keep `bg-callout` and take `border-callout-border`
rather than becoming `bg-card` — their copy is `text-accent`/`text-accent-dark`,
which this app only ever puts on the callout tint. And the Net figure
deliberately did **not** take the plan's `text-[15px]`: `tabular-nums` aligns a
column at one size only, so a larger Net puts its decimal off the x-coordinate
of every row it sums. Both are commented at the line.

Three things about the dev box. **The dev database changed twice.** M2's walk
needed states that did not exist, so an achieved goal ("Japan 2027", S$10 of
S$10), an August budget (Food capped at S$800) and a S$2.00 "Kopi" expense on
2026-08-28 were created through the UI. M3's walk needed a household with at
least one bill and all of them paid to reach "All caught up" at all, and neither
household had any, so a monthly **"Internet" bill** (S$59.90, Utilities, next
due 2026-08-15, ticked as a subscription) was created and marked paid — which
**writes a real S$59.90 expense to the ledger**. That is why Net worth reads
−S$5,921.90 rather than M2's −S$5,862.00, and why Budget's Utilities row shows
S$59.90 against a S$0.00 cap. **Check which household you are signed into, and
which round's figures you are comparing against, before concluding a screen is
wrong.** And **`noValidate` is still absent from fourteen of the app's fifteen
forms** — only `TransactionModal` was fixed; `GoalModal` answered the walk with
Chrome's own "Please fill out this field.". That sweep is named in the spec's
Out of scope and is real work, not a hypothetical.

---

## 2. Running it

```bash
make dev      # everything, logs tailed — http://localhost:5173
make seed     # the design's household; prints Christine's invite URL
```

`make seed` gives you Andreas signed-in-able with a known development password,
a pending invite for Christine, Kayla (12) and Ethan (8) as credential-less
members, the three builtin spaces, and notification preferences.

Bare `make` lists every target. The ones you will actually use:

| | |
|---|---|
| `make dev` / `make down` | start and stop |
| `make dev-local` | API and web as native processes for a debugger; infra stays in Docker |
| `make seed` | the household above; refuses to run outside `APP_ENV=development` **and** against a non-local database |
| `make reset-password EMAIL=…` | prompts on stdin, never a flag; also revokes that user's sessions |
| `make unlock-household` | clears a lockout without waiting 15 minutes |
| `make migrate-new NAME=…` | runs through the pinned dev image, not a host binary |
| `make lint` | arch lint, frontend typecheck, eslint, `go vet` |
| `make test` | Go suite (needs Docker) plus 465 frontend tests |

**Docker is colima on the original machine.** The Go suite uses testcontainers
and needs:

```bash
export DOCKER_HOST=unix:///Volumes/Oink_Machine/.colima/default/docker.sock
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock
```

This machine can also run Docker Desktop at the same time. If it is up, it
silently wins host ports 5173/8080/8025 out from under colima's stack, so
`make up` succeeds, `docker compose` manages colima's containers, and the
browser and every `curl` still talk to whatever Docker Desktop published —
check `docker ps` on both engines before concluding the code is broken.

**Host edits to `web/src/**` do not reliably reach the running dev server.**
Vite runs inside `hearth-web-1` against a bind mount, and chokidar misses the
host's filesystem events, so a change simply never arrives: the browser shows
the old bundle, and a hard reload changes nothing because the server is still
serving what it last built. `docker restart hearth-web-1` fixes it instantly.
Bills' own page task lost time to this before recognising it — the symptom
reads exactly like a change that did not compile, which is the wrong thing to
go looking for.

Mailpit catches all outbound mail at `http://localhost:8025`. Its API is
easier to drive than its UI:

```bash
curl -s "http://localhost:8025/api/v1/messages?limit=1"
curl -s "http://localhost:8025/api/v1/message/<ID>"
```

Magic links, invites and sign-up links all land there in development.

### Driving the app in a browser

Every "done" claim here needs a browser walk (see §1 and `CLAUDE.md`). Six
things cost previous walks real time, so they are written down rather than
rediscovered:

- **Both MCP browsers refuse to attach if a previous session left one
  running.** The M2 handover said to reach for the Playwright MCP when
  chrome-devtools answers `The browser is already running for
  .../chrome-profile. Use --isolated` — but M3 found Playwright answers the
  same way about its own profile (`Browser is already in use for
  .../ms-playwright-mcp/mcp-chrome-f265c67`). The orphan is an
  automation-profile Chrome, not the user's, and killing it scoped to that
  profile cannot touch a personal browser:

  ```bash
  pkill -f "user-data-dir=/Volumes/Oink_Machine/Library/Caches/ms-playwright-mcp/mcp-chrome-f265c67"
  ```

  Match on the `--user-data-dir=` path, never on `Google Chrome` — that would
  close whatever the person at the keyboard has open.
- **`touch` every file you edited before you trust what the browser shows.**
  Vite's watcher does not reliably see writes on this volume. M3 edited five
  files, four reached the browser and one did not, so the page rendered the
  new markup wrapped around an empty string — with the file correct on disk, a
  hard reload changing nothing (the staleness is server-side) and the
  type-check passing, so every signal pointed at the code being wrong. Check a
  suspect module by asking Vite for it directly, which is the discriminating
  test: `curl -s http://localhost:5173/src/<path> | grep <the new symbol>`.
  `docs/LEARNING.md`'s Tooling section has the fuller case, including the
  earlier deployment-round one.

- **Clicking by element reference goes stale.** A reference captured in the
  same batch as a navigation often points at a tree React has since
  re-rendered. Sign-out in particular has never fired reliably that way; it
  works immediately via page script:
  `document.querySelector('button[aria-label="Sign out"]').click()`.
- **React controlled inputs ignore synthetic typing.** Set them through the
  native setter and dispatch the event, or the component's state never
  updates:

  ```js
  const set = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, 'value').set;
  set.call(el, 'value'); el.dispatchEvent(new Event('input', { bubbles: true }));
  ```

  Use `'change'` and `HTMLSelectElement` for a `<select>`.
- **Screenshots are scaled and easy to misread.** A walk twice concluded an
  element was missing when it was rendered far to the right at a tiny scale.
  Assert on numbers — `getBoundingClientRect()`, or read `innerText` in page
  script — and keep screenshots as the record, not as the evidence.
- **Two byte-identical before/after screenshots mean the change did not
  land.** That exact failure is in `docs/LEARNING.md` from the finance-fixes
  branch. Compare hashes rather than eyeballing; the interim Overview's walk
  did (`shasum -a 256`, five files, five distinct hashes).

Creating the non-owner states a walk needs, since the seeded owner exercises
only one of them:

```bash
docker compose exec api go run ./cmd/adminctl create-invite \
  --email=someone@example.test --name=Someone --role=limited \
  --capabilities=money --inviter-email=<an existing member's address>
```

`adminctl` with no arguments prints its real subcommands; `--help` is not one
of them.

---

## 3. The shape of the code

```
api/
  cmd/api/            wiring only — read config, open the pool, build the router, serve
  cmd/adminctl/       seed, reset-password, unlock-household, create-invite, prune
  internal/domain/    rules. Imports the standard library and nothing else.
  internal/usecase/   services + every port interface (ports.go)
  internal/adapter/   http, postgres, crypto, mail, clock, fx
  migrations/         goose
web/src/
  api/client.ts       apiFetch — the only way the app talks to the server
  components/         generic primitives only (Modal)
  features/           auth, shell, settings, money, placeholder
  routes/router.tsx   the route tree
```

**Dependencies point inward, and `make lint-arch` enforces it mechanically** —
including in test files. `internal/domain` may import stdlib only;
`internal/usecase` may add `internal/domain`. Everything else lives in an
adapter. The lint runs a real build first, because `go list` alone tolerates
breakage the compiler rejects.

`internal/usecase/ports.go` is the contract. Read it before writing a service or
a repository; it carries doc comments that are load-bearing, not decorative
(the `""` ⇄ SQL NULL convention, the transactional-accept warning).

---

## 4. What to do next

**The build order changed once already: self-serve sign-up shipped ahead of
slice 2.** The original four-slice order below (Money, then Marriage, then
Family, then Overview) was dependency-driven and none of that changed. Sign-up
is a separate piece of work that cut the line for a different, recorded
reason: its own spec's decision 1 ships it before the platform admin console
that will manage it (a deferred, separate spec) so it earns real usage first
— and a household has to be able to exist before there is anything for that
console to administer.

**Slice 2 (Money) is done.** Accounts, Transactions, Budget, Goals and
Bills — all five features — are code-complete, reviewed, and now all
**walked in a browser**, each 15 of 15. Goals' own walk ran on 2026-08-01,
finding one real defect at criterion 12 and fixing it mid-walk (§1;
`docs/superpowers/plans/2026-08-01-hearth-goals-verification.md`). Bills' own
walk ran on 2026-08-10, finding one real defect at criterion 14 and fixing it
mid-walk (§1; `docs/superpowers/plans/2026-08-09-hearth-bills-verification.md`).
~~**The next work is not a feature — it is the first production deployment**~~
**Done, 2026-08-15.** The reasoning below held and is worth keeping, because
it is the argument for doing this kind of work before the next slice rather
than after: a stranger could already sign up and use the product, so there was
something worth deploying, and every deployment problem is cheaper to find with
zero users than with ten. Marriage is thirteen features and several weeks;
shipping it into a stack nobody had run in production would only have meant
meeting the same problems later with more code on top of them.

That call paid off immediately. Three things were wrong in ways only a real
deploy could have shown: the machine the ADR named had been renamed out of
existence by Hetzner, the region it chose does not sell the cheap instance line
at all and cost more than the AWS bill the move was meant to escape, and the
runbook still instructed the operator to configure a mail relay that ADR 3 had
already made impossible. All three were found at the order form or on the box,
none of them by a test.

**Marriage's first feature, Retros, is code-complete, reviewed and now
walked.** Fifteen code tasks deep, every one reviewed clean including fix
rounds (a sixteenth task wrote the documents this section is part of; a
seventeenth ran the walk below), `docs/FEATURE_TRACKER.md`'s
Marriage rows brought current in the same round as this document
(2026-08-17): retro history with mood, the twelve-month mood chart, the
single retro view (went well, was hard, actions, notes) and the Start/Edit
modal (mood, the ten-minute money check-in, the action composer and the
carry-over offer) are all ✅; carrying an unfinished action forward is ✅; and
deleting a draft retro — a real gap this section once named, a backend and
hook with **no screen that called either** — is ✅ too, closed the same round
`RetroModal.tsx` was given a Discard-draft control (`4d719b8`). **Retros' own
fifteen-criterion browser walk (Task 17) has now run and passed, 15 of 15**
(2026-08-18), the same bar every Money feature was held to, recorded in
`docs/superpowers/plans/2026-08-16-hearth-retros-verification.md`. No product
defect needed a code fix; two deliberate divergences from the design spec's
own prose came out of it and are recorded in `docs/FEATURE_TRACKER.md`'s
Marriage section rather than repeated here.

**With Retros' and Vision's walks both passed, the next work is Agreements
— Marriage's last feature.** That was always the order (spec: "Retros first because it is the smallest complete
loop... Agreements last because propose → both sign... carries a product
question that deserves its own conversation"). **Agreements must settle one
question before its spec can be written: what "both sign" means in a
household with one owner.** `domain.ValidateMembershipChange` refuses
`CapMarriage` to a limited member, so the signing set for any agreement is
exactly the household's owners — and self-serve sign-up provisions exactly
one owner per household, with a partner arriving later, if at all, only
through an accepted invite. A design that requires two signatures has
nothing to compare the first one against until a second owner exists; ask
before Agreements' spec is written, not while it is being built, what the
screen shows a one-owner household in the meantime.

Once Retros' walk closes it out, the production deployment remains the other
settled item: ten of twelve criteria pass, backups run nightly and the
escrow has been exercised. What remains on the install is now **one item**:
a first run of `adminctl unlock-household` on a calm day (criterion 8 —
unrun because the agent's sandbox blocked the SSH, not because of the box;
the exact command is in
`docs/superpowers/plans/2026-08-10-hearth-production-verification.md` under
"What is outstanding"). The other two this line used to name are closed and
were closed after it was written: uptime monitoring exists and its alarm was
fired on purpose (`315eb7e`), and both R2 dashboard rules — the 30-day
Bucket Lock and the 90-day lifecycle expiry — were verified by exercising
them from the box, not merely configured (`22a3105`). Criterion 7's lockout
half is still unrun too; criterion 3 stays deferred under
[ADR 3](adr/0003-mail-stays-on-the-box.md).

### Deploying to production — the decision and what it still needs

Two ADRs carry the reasoning; read them before changing any of it:

- **`docs/adr/0001-optimise-for-exit-cost.md`** — the product owner intends to
  run Hearth for roughly forty years. No hosting provider is a forty-year bet,
  so hosts are chosen for how cheaply we can *leave* them, not how long they
  are expected to last. That is why plain Postgres, a static Go binary and our
  own identity layer are load-bearing properties rather than incidental ones,
  and why Vercel and Supabase were rejected on architecture rather than price.
- **`docs/adr/0002-first-production-host.md`** — the concrete purchase: a
  Hetzner CX23 in Falkenstein running the existing Compose stack, Caddy in
  front for automatic TLS, Postgres on the same box, nightly plain-SQL
  `pg_dump` off-provider, Resend's free plan for mail. Roughly S$10–13/month.

**Nothing has been deployed yet.** Seven things were listed here originally;
three are now done and struck below, so **four still stand** between the
decision and a live install — items 4, 5, 6 and 7, in this order. Item 5 is
half closed rather than open, and says exactly which half. The struck items
stay visible on purpose: what has already been settled is as useful to a
newcomer as what has not.

1. ~~**A production Compose file.**~~ **Done.** `deploy/docker-compose.prod.yml`
   declares the six production services on one private network with a fixed
   `172.28.0.0/16` subnet, reads every value from `deploy/.env`, and takes the
   project name `hearth-prod` so it cannot share a volume with the development
   stack. `deploy/Caddyfile` and `deploy/README.md` ship beside it.
   (`docker-compose.yml` remains development-only: `air` hot reload, bind
   mounts, Mailpit, Postgres published on the host, the password literally
   `hearth:hearth`, and no `.env` interpolation anywhere. `sslmode=disable` is
   **not** on that list: Postgres stays on the Compose bridge network and is
   never published, so the connection never crosses a network and requiring TLS
   would mean issuing certificates for a link that does not leave the machine.
   An earlier draft of this section listed `sslmode=require` as a production
   change; the deployment spec's decision 9 corrects it.)
2. ~~**An image that can administer production.**~~ **Done.** `api/Dockerfile`
   carries a third target, `admin`, on the same distroless base as `prod`,
   holding `goose` and `adminctl`. `docker-compose.prod.yml` runs it as a
   one-shot `migrate` service ahead of `api`, and as a `profiles: [manual]`
   `admin` service for `unlock-household`, `reset-password`, `create-invite`
   and `prune` — every command written down in `deploy/README.md`.
3. ~~**`set_real_ip_from` in `web/nginx.conf`.**~~ **Done.** `web/nginx.conf`
   carries `set_real_ip_from 172.28.0.0/16`, `real_ip_header X-Forwarded-For`
   and an explicit `real_ip_recursive off`. The trusted range is the whole
   compose subnet rather than Caddy alone, and `docs/SYSTEM_DESIGN.md` §1 says
   why that is accepted. Proven with two containers getting two independent
   budgets, and forged `X-Forwarded-For` / `X-Real-IP` / `True-Client-IP`
   headers failing to open a third.
4. **`APP_BASE_URL` set to the public HTTPS origin.** It is embedded in every
   magic link, invite and sign-up mail; wrong, and every mailed link points at
   `localhost`.
5. **Backups — half done.** Both scripts exist and are real: `deploy/backup.sh`
   dumps in plain SQL, gzips, encrypts with `age` and uploads off-provider,
   pinging its heartbeat only after a successful upload; `deploy/restore.sh`
   reverses it into a database you name, refusing any DSN that looks like the
   live one. A restore **has** been performed once — on a laptop, into a
   throwaway container, with the restored row counts matched against the
   source. What remains is only the part that needs the box: the nightly cron
   actually running on a schedule, and one restore decrypted with the
   **escrowed** copy of the key rather than the author's own, since an escrow
   that has never been used is a hope and not an escrow.
6. ~~**DNS**: the A record, plus Resend's SPF, DKIM and DMARC records.~~
   **Reduced to the A record**, and it exists: `oink.mywire.org` resolves, TTL
   120, currently pointed at the owner's own network and repointed at the box
   when there is one. The SPF/DKIM/DMARC half is **cancelled for now** —
   `docs/adr/0003-mail-stays-on-the-box.md` records why. Dynu refuses `TXT`
   records on free third-level hostnames under 30 days old, so DKIM cannot be
   published, so no hosted relay will verify the domain. Mail runs on Mailpit,
   on the box, read over an SSH tunnel. Caddy is unaffected: its ACME challenge
   is HTTP-01 over port 80 and needs no DNS record.
7. **A browser walk against the deployed install**, to the same standard every
   feature here has been held to. First-run is exactly where these walks have
   found things before. **Two of its twelve criteria cannot run under ADR 3**
   and are deferred rather than passed: criterion 2 (sign-up from a phone on
   mobile data) and criterion 3 (mail arrives in a Gmail inbox, not spam). Run
   both the day a real domain lands — criterion 3 in particular, since it is
   the only thing that tells you whether outbound mail works at all.

**Slice 5 (Overview) is the exception to its own rule, deliberately.** The
original order put it last because it only aggregates, so building it early
means stubbing everything it reads. That still holds for the *designed*
Overview. What shipped on 2026-08-01, and grew twice since, is an interim
page built strictly on what already exists — two of the design's **seven**
cards at first (recounted directly off the mockup while correcting this
paragraph: the money row of four, Marriage's "Next retro", "This week" and
"Vision 2026" — the header's own "+ Add" button is not a card, and the
figure this paragraph carried before named eight), a third (goals on track)
the same day once Goals shipped, a fourth (next bill due) on 2026-08-10 once
Bills did, a fifth (the next retro, with its own open action count) on
2026-08-16 once Retros did, and a sixth (Vision 2026, plus the check-in
strip inside the next-retro card, which is a line the design draws inside
that card rather than a seventh card) on 2026-08-29 once Vision did — no
stubs, no invented figures, taken early because `/` was showing every
household "Arriving in slice 5" on every visit, established households
included. The one remaining card ("This week" agenda) still waits on
Family, and the same route and the same component grow into it.

Each slice gets its own spec → plan → implementation cycle, the same way these
did. The originating spec for slices 0–1 is
`docs/superpowers/specs/2026-07-26-hearth-foundation-design.md`; self-serve
sign-up's own is
`docs/superpowers/specs/2026-07-27-hearth-self-serve-signup-design.md`;
Accounts' own is `docs/superpowers/specs/2026-07-28-hearth-accounts-design.md`;
Transactions' own is
`docs/superpowers/specs/2026-07-29-hearth-transactions-design.md`; Budget's
own is `docs/superpowers/specs/2026-07-30-hearth-budget-design.md`; Goals'
own is `docs/superpowers/specs/2026-08-01-hearth-goals-design.md`; Bills'
own is `docs/superpowers/specs/2026-08-09-hearth-bills-design.md`. The
completed plans beside them are worth skimming for house style before writing
Marriage's.

### What Accounts through Bills closed, across all five Money features

Three things a prior review flagged as "must not be forgotten" before slice 2's
first task. Accounts closed the first, upheld the second, and pinned the start
of the third; Transactions pinned two more figures of its own, Budget pinned
three more, Goals pinned two more, and Bills pinned the last three the design
shows anywhere in Money:

1. **`requireCapability` middleware exists and no route uses it — closed.**
   The spec promised the server enforces capabilities independently of the
   UI; until Accounts, that promise was vacuous. `GET /api/v1/accounts` and
   its four write routes are now gated on the `money` capability (reads) and
   `money` plus owner (writes); Transactions, Categories, Budget, Goals and
   Bills go further — `money` **and** owner gate their reads too, not just
   their writes, because a ledger, a budget screen, a goal card or a bill row
   with every figure blank reads as broken rather than merely restricted
   (`docs/SYSTEM_DESIGN.md` §4). The route-walk test matrices under
   `api/internal/adapter/http/` cover all five shapes.
2. **Money is `int64` minor units plus an ISO 4217 code, everywhere — held.**
   `domain.Money` refuses to mix currencies; `AccountService.Summary`,
   `TransactionService.MonthSummary` and `GoalService.List` all convert into
   the household's primary currency before summing, for exactly that reason
   (`docs/LEARNING.md`, pattern 12). Budget held the line too, once under
   real pressure: the frontend's first 50/30/20 template split multiplied
   expected income by a float literal (`incomeMinor * 0.3`) before flooring,
   which drifted by a minor unit on at least one real income figure — caught
   in review, fixed to integer-first arithmetic (`docs/LEARNING.md`, Domain
   and money catalogue). Bills held it under a different pressure: it
   stores no currency of its own at all, deliberately — the pay-from
   account's currency is authoritative, resolved through the same
   `AccountLookup` `TransactionService.Create` already uses, so a bill
   cannot come to disagree with the expense it eventually writes
   (`docs/SYSTEM_DESIGN.md` §5). No `float64` survived in a monetary path
   anywhere in the stack, on any of the five features.
3. **The derived figures the design shows anywhere in Money are now all
   pinned and built.** Net worth (Accounts). `Count` and `Spent`, expenses
   only (Transactions). `66% used`, `S$137/day left` and `on pace to save
   S$1,780`, all Remaining-based rather than a run-rate projection (Budget
   spec decision 2 — a projection can contradict the mockup's own numbers
   with the mockup's own data). `X of Y on track` and the manual move of
   unspent budget into a nominated goal, both pinned in Goals' own formula
   table (`docs/superpowers/specs/2026-08-01-hearth-goals-design.md`) — the
   rollover deliberately not the design's automatic toggle, since a stored
   setting that acts only when clicked would read as automatic, the same
   dishonesty Budget's own spec had already refused for this row (Budget
   decision 1, Goals decision 4); the manual button ships instead
   (`docs/FEATURE_TRACKER.md`'s Budget row). And the last three anywhere in
   Money, Bills' own: the due-soon/later split, autopay's badge and copy,
   and the subscriptions monthly/annual totals, all pinned in Bills' own
   formula table (`docs/superpowers/specs/2026-08-09-hearth-bills-design.md`).

**Marriage does not inherit a clean slate from this section, and Retros is
what settled item 1's shape for the whole area.** Money closed all three
items above across five features in a row; Marriage is a different domain
(mood, retros, agreements, none of it money), so items 1 and 2 did not
transfer as written. Item 1 is now decided, not still open: `/marriage/retros`
is gated `requireCapability(marriage)` stacked on `requireOwner` (spec
decision 11 — redundant with `domain.ValidateMembershipChange` today, kept
so the route does not lean on an invariant enforced only one layer down),
the identical shape the money group already used, and `docs/SYSTEM_DESIGN.md`
§4 carries the reasoning. Vision and Agreements inherit that shape directly —
neither needs its own capability-gating decision, only its own route added
to the same guarded group. Item 2 still does not transfer: Marriage carries
no monetary figures to keep `int64`-honest. What *does* transfer is the
discipline behind item 3, and Retros already exercised it once (every figure
on the Retros screen is pinned in its own spec's formula table before any of
it was built): an implementer who invents Vision's or Agreements' own
derived figures — mood trends, agreement version diffs, whatever Vision's
"on track" turns out to mean — without a decision recorded first in that
feature's own spec is building on sand, the same warning this section has
now enforced across six features running.

### The seams slice 2 will use

- **Bank sync is not a port anyone has built.** SGFinDex is restricted to
  licensed institutions, so `BankSyncProvider` does not exist, and Accounts —
  Money's first feature — shipped without it: manual entry needs no port at
  all, and a port with one implementation and no second caller is the wrong
  shape. It arrives when CSV import gives it a second implementation to
  abstract over. The design's 3-step Link-account modal was cut to one step
  for the same reason: a chooser between "connect a bank" (permanently dead)
  and "manual account" teaches nothing with only one live branch, so
  `+ Add account` opens the manual form directly.
- **`AccountView.Balance` is a real sum, computed in the repository's SQL, not
  in a service.** Transactions is what made it one: before, `Balance` copied
  `opening_balance_minor` because there was nothing to add. See
  `docs/SYSTEM_DESIGN.md` §5.
- **A transaction is hard-deleted; an account never is.** Nothing references
  a transaction, so archiving one would only be a screen nobody asked for.
  Budget's category archiving follows the *account* pattern instead — a
  category is referenced by transactions, so it archives rather than deletes,
  the same reasoning, applied to a different table.
- **The sidebar renders from `me.spaces`**, filtered and ordered by the server.
  Accounts added a real page under the existing Money space (Finances,
  replacing its placeholder); Transactions added a second, real sibling route
  (`/money/transactions`) under the same guard rather than the placeholder's
  catch-all. The finance-fixes branch then gave the sidebar itself the
  design's grouped form: Money renders as an uppercase label plus a
  Finances/Transactions link pair, with the accent following the active
  route.
- **`components/Modal`** is the shared primitive. Roughly fifteen modals across
  slices 2–4 build on it. It reaches genuine `:modal` state — do not
  reintroduce a declarative `open` attribute.
- **`stubFetchRoutes`** matches on method and URL and throws on an unregistered
  request. Use it for every frontend test; a stub that ignores the URL has
  silently passed broken code twice in this project.

---

## 5. Remaining items

`docs/superpowers/plans/2026-07-26-hearth-follow-ups.md` is the full list, with
the reasoning for each. The headlines:

### Mobile: eight things measured and deliberately left

None of these blocks use of the product on a phone; each was measured, and each
was left for a reason worth reading before "fixing" it.

- **Four touch targets stay under the 44px floor.** `MembersPanel`'s role
  toggle (24.5px — raising it outgrows the 34px avatar beside it); the sixteen
  `ToggleSwitch` instances (23px — dense usages sit 6–14px apart, so larger
  targets would overlap each other, which is worse for hit accuracy than a
  small one); `BillRow`'s Mark paid / Archive / Restore / Undo (16.5px —
  raising them grows the stacked mobile cluster from ~89px to ~144px against a
  ~40px left block, and doing it properly needs the row restructure the spec
  forbids); and `BudgetRolloverCard`'s "Move into goal", which is genuinely
  inline mid-sentence so `min-height` does not apply to it at all.
- **`BudgetPage`'s `‹`/`›` month arrows were raised on height only** (`h-11`,
  no `w-11`) because the full square wraps "Edit budget" onto two lines in that
  row's real state. The restored desktop arrow is 3.94 × 19.5px — a
  pre-existing small target this round declined to widen, not one it created.
- **`<main>` stays `inert` if the viewport crosses `lg` while the drawer is
  open** — the hamburger and backdrop are both `lg:hidden` by then. Recovery is
  Escape *or* any sidebar click (`<nav>` is a sibling of `<main>` and never
  inert). Reachable by tablet rotation; fixing it properly means reintroducing
  the JS viewport-watching `NavDrawer`'s design comment deliberately avoids.
- **Two pre-existing `md:` breakpoints survive** in `BudgetStatCards.tsx` and
  `FinancesPage.tsx`, against the two-breakpoint (`sm`/`lg`) convention the
  rest of the app follows.
- **`NewSpaceModal`'s pill row stays `grid-cols-2` unconditionally.** Measured
  at 320px with no overflow; it holds two short pills.

### Decisions, not defects — do not "fix" these without a conversation

- **The lockout is household-wide, and that leaks which addresses share a
  household.** Someone who knows one member's address can confirm a second by
  watching the attempts countdown. Accepted deliberately: the design's copy says
  the household locks. Revisit only if the lock's scope changes.
- **The lock is uncapped**, so repeated guessing can hold a household locked
  indefinitely. Accepted: magic link is deliberately ungated and is the way back
  in.
- **Sign-up's rate-limit numbers (per-IP 5/hour, global 1000/day, reset at
  midnight) were changed deliberately by the product owner**, after a
  whole-branch review found the previous pair (10/hour, 200/day) did not
  compose — one IP alone, inside its own hourly budget, could exhaust the
  global ceiling, after which sign-up silently mailed nothing platform-wide
  for up to a day. The two failure modes are asymmetric: a per-IP `429` is
  cheap and self-announcing, a tripped global ceiling is silent and
  platform-wide, so the global number must stay one that legitimate traffic
  never approaches. Do not change either number without re-reading
  `docs/LEARNING.md`'s pattern 11 and re-checking
  `TestSignUpRateLimitsCompose`, the test that asserts the two stay composed.
- **`RollOver` moves `Remaining` even on a month with excluded
  transactions, and that can read higher than the household's true unspent
  figure.** `BudgetService.Month`'s `Remaining` is `Budgeted − Spent`, and
  `Spent` excludes any expense with no available exchange rate — a
  pre-existing property of `Month`, not something Goals introduced. The
  owner's ruling (2026-08-01): move it, but say what was excluded. The
  rollover offer names the excluded count next to the button whenever it is
  positive (`BudgetRolloverCard.tsx`, commit `8a1114b`), and the button
  stays enabled — this is information, not a refusal. Do not "fix" this by
  blocking the button on a positive exclusion count without a further
  product conversation; see `docs/SYSTEM_DESIGN.md` §5's Goals flow for the
  full reasoning.
- **Autopay is a display flag; nothing pays a bill by itself.** Every bill,
  autopay or not, is marked paid by a person clicking a button — there is no
  scheduler anywhere in this codebase, and Budget decision 1 and Goals
  decision 4 both already refused to invent one for their own features
  (Bills spec decision 3). The accepted cost is real: a bill the bank has
  already auto-paid sits unpaid on screen, and can go overdue there, until a
  person confirms it. The flag exists precisely for that moment — it is what
  tells the household whether an overdue row is an errand or a five-second
  click — not to make the row disappear on its own. Do not "fix" the
  overdue-while-autopaying state by auto-stamping a payment on read: that
  writes a row during a `GET`, invents money the bank may not actually have
  taken (a failed GIRO would show as paid), and the amount would be a guess
  that moves net worth.

All five are documented in the code at the point a future editor would change them.

### Worth doing when convenient

Four items previously listed here are now done. Self-serve sign-up closed
three: `preAuthPathPrefixes`/`publicRoutePrefixes` became one
`web/src/routes/publicRoutes.ts` backed by a router-walk test; non-ASCII
display names now get a correct avatar initial (`initialOf` slices the first
rune and case-folds through `cases.Upper`, and `avatar_initial` widened to
`text` for the rare expansion case); and backend currency validation now
checks membership in a real ISO 4217 allowlist (`domain.ParseCurrency`) rather
than format only, so `ZZZ` is refused. Accounts closed the fourth:
`requireCapability` was unused and carried a deadline (§4 above) — it now
gates the accounts routes, and the route-walk matrices exercise it.

**Inherited from the interim Overview (M2), none of it blocking:**

- **Pending invites are exposed nowhere, and one checklist step is missing
  because of it.** `InviteService.Create` writes an emailed invite to the
  `invites` table only; `GET /household/members` reads `memberships` joined to
  `users`, so a pending invite is not a row there, and no endpoint lists one.
  Overview's setup checklist therefore has three steps rather than four — an
  "invite your partner" step could only tick once the partner *accepted*,
  leaving an owner who had just invited someone looking at an unticked step
  whose link goes to a Settings page showing no trace of the invite. The
  product owner chose to drop the step rather than ship that (2026-08-01), on
  the precedent of Budget spec decision 1. **A `GET` for pending invites is
  the follow-up**: it closes the checklist step *and* gives Settings something
  to show, which is the larger gap of the two.
- **The not-found page is a dead end, and that is a product decision, not a
  bug.** M1 deleted the Marriage and Family routes, so their URLs reach
  `notFoundComponent` — which sits *above* the authenticated shell, so the 404
  renders with no sidebar and no link home, and a signed-out visitor with an
  old bookmark gets bare "Page not found." text instead of the sign-in screen.
  Both are documented in `router.tsx`'s header comment and pinned by
  `router.test.tsx`. Only URLs that never shipped to a customer are affected.
  The smallest fix is a `<Link to="/">` inside `notFoundComponent`; making the
  signed-out case redirect needs the route moved under the shell, which is
  structural. **Ask before doing either — it is the owner's call.**
- **`.claude/worktrees/transactions/` is a registered git worktree holding
  pre-M1 copy.** It affects neither lint nor tests, but it is a live decoy for
  exactly the repo-root grep sweeps `docs/LEARNING.md` now warns about — a
  re-sweep of the audience copy gets eight false positives from it. Removing
  it deletes a directory, so it needs the owner's consent:
  `git worktree remove .claude/worktrees/transactions`.
- Three cosmetic items parked at M1's final review, any of which is fair game
  if you are already in the file: in `docs/SYSTEM_DESIGN.md` §7, the
  three-cases-not-two paragraph about `SPACE_PAGES` contains the parenthetical
  "no space is in that state today" — "that state" has no unambiguous
  antecedent (loose, not wrong). Quoted rather than given a line number
  deliberately: the number it used to carry was already stale by the time this
  entry moved here;
  `SignUpCompleteScreen.tsx:43,47`, two action labels still reading "Create a
  household" for a click that navigates to the email form — defensible as a
  call-to-action, and a fourth phrasing that escaped the same sweep that
  missed "you both".
- **The interim Overview's limited-member panel is gated on
  `accounts.isSuccess`, and that gate has no test.** It exists so the panel
  does not flash at an owner while `/accounts` is still in flight (`summary`
  is equally undefined then). Mutating it to the looser condition leaves every
  test green. A test would have to assert a transient loading state, which is
  the shape `docs/LEARNING.md` records as having produced a worthless guard
  test before — so it was left uncovered deliberately rather than covered
  badly.

**Deferred whole out of Goals, by its own spec's decision, none of it
blocking:**

- **Automatic monthly contributions, and any scheduler, do not exist.**
  Contributions are entered by a person; nothing in this codebase runs on a
  clock yet. The design shows "S$2,050 auto-saved on the 1st of each month"
  and "next transfer Aug 1" — neither ships. Inventing this codebase's first
  scheduler inside a feature that arrived with four undefined figures was
  judged the wrong trade (Goals spec, "This is the M-sized Goals,
  deliberately"). **The follow-up is its own later spec**, written once real
  households have goals to point contributions at — there is no scheduling
  infrastructure anywhere else in the product to build it on top of yet.
  **That same missing scheduler is why four Settings rows moved ✅ → 🟡 on
  2026-08-16**: bill due reminders, overspend alerts, the monthly retro
  reminder and the weekly family digest are all stored, served and editable,
  and none of them is ever sent — `usecase.Mailer` has three methods (magic
  link, invite, sign-up) and no caller reads `notification_preferences` to mail
  anything. The design's copy promises delivery ("Bill due reminders (3 days
  before)"), so the toggles are a promise the product does not keep. Whoever
  writes the scheduler spec should cover both — automatic contributions and
  rollover, and these four — since they are one missing piece, not two.
  `docs/LEARNING.md` pattern 15 carries the finding.
- **Automatic month-end rollover does not exist; only the manual button
  does.** The design's "Roll unspent into savings" toggle implies money
  moves by itself at midnight on the 1st; Goals decision 4 refused that
  shape for the same reason Budget's own spec already refused it once
  (Budget decision 1) — a setting that acts only when clicked reads as
  automatic, and that is a dishonesty this product's own "· not built"
  convention exists to prevent. The accepted cost: a household that never
  revisits last month's Budget page never rolls anything, and nothing
  happens silently. **The follow-up is the same later spec as automatic
  contributions** — the two are the same missing piece (a scheduler this
  product does not have yet), not two independent gaps.

- `apiFetch` has no timeout or abort, so a request that never settles leaves its
  control disabled indefinitely.
- `CurrencyPanel` and `NotificationsPanel` are correct but unprotected: neither
  has a test that would catch a regression to the non-awaited invalidation.
- **`make up` can silently skip a migration.** `api` declares `depends_on:
  migrate` with `service_completed_successfully`, but Compose only
  re-evaluates that condition when it recreates `api` — so a stack left
  running across a newly added migration keeps its already-succeeded
  `migrate` container and never reruns it. `make dev-local` already runs
  `make migrate` explicitly before starting anything; `make up` and a bare
  `docker compose up` do not. Found while grounding this slice's own docs
  update, not by a test — `make down && make up` (which forces recreation) or
  an explicit `make migrate` sidesteps it for now.

Transactions' reviews flagged nine more, judged non-blocking at the time.
Five are worth doing before they compound; four were judged noise and are
left out of this list on purpose (a redundant `var _ AccountLookup`
assertion that duplicates a check `main.go` already pins; the ordering of
`requireCSRF` before or after `requireOwner` on transactions versus accounts,
which has no observable effect either way; `seedSize = 13` coupling the
adapter to the starter-set count, which fails loudly and is already commented
where it would bite; and no dedicated index on `categories(household_id)`,
which needs none — `UNIQUE (household_id, name)` already puts `household_id`
as that index's leading column, so an equality lookup on it alone is served
by the same index a name lookup uses):

- **The goose `Down` migration for `00005_transactions.sql` is correct by
  inspection but no test has ever run it.** Every other migration in this
  project is in the same position, `00006_budgets.sql` (Budget's own) now
  included; this is not a new gap, just a fresh reminder of an old one.
- `api/internal/adapter/http/api_test.go` split by feature area is
  **done** — Budget's Task 1 did it before its own routes could add a fifth
  block to the 2036-line file: `auth_api_test.go`, `household_api_test.go`,
  `accounts_api_test.go`, `transactions_api_test.go` and
  `budget_api_test.go` now each own their feature's route-walk matrices,
  with `api_test.go` itself left holding only the shared test harness.
- `web/src/features/money/TransactionsPage.tsx` is still over 500 lines
  doing fetch orchestration, pagination, PATCH-body translation and row
  rendering together — **not split**. Budget dodged repeating the shape
  rather than fixing it: its own spec (decision 11) put `BudgetPage.tsx`'s
  fetch orchestration in a dedicated hook (`useBudget.ts`) from day one
  instead of copying `TransactionsPage.tsx`'s pattern and splitting later,
  so `BudgetPage.tsx` itself never grew the debt. `TransactionsPage.tsx`
  itself is exactly as it was; still worth splitting on its own before a
  third page copies it wholesale.
- `RecentTransactionsCard` has its own date formatter, duplicating one
  `TransactionsPage` already has. Small today; this is exactly the "fixed in
  one place, left in the sibling" shape pattern 1 warns about, so worth
  merging before a date-formatting bug has two places to hide in.
- Only two of the four `clearReceivedAmount` input combinations have a test
  (see `docs/LEARNING.md`'s Task 16 entry for what the other two would need
  to assert). Cheap to close while the PATCH-translation logic is still
  fresh in mind.

### Before this is deployed anywhere real

> **It is deployed now — 2026-08-15.** This list is kept because it is the
> record of what had to be true first, and because most of it is still live
> advice for the *next* box. Items proven on the running install are marked
> below. What is emphatically **not** closed: backups (§1), which were never on
> this list because the list was about what blocks a deploy, not about what
> makes one survivable.

- ~~**TLS termination in front is mandatory.**~~ **Satisfied in production.**
  Caddy terminates TLS and issued for `oink.mywire.org` on the first attempt;
  verified from outside the box, and the session cookie confirmed on the wire as
  `HttpOnly; Secure; SameSite=Lax`. The underlying constraint is unchanged and
  still bites any new environment: cookies are `Secure` outside development
  while nginx listens on plain `:80`, so without TLS in front the browser never
  returns the session cookie and everything 401s. The API logs a warning at
  startup; `.env.example` and `web/nginx.conf` say so.
- **SMTP now takes TLS policy and credentials from configuration**, defaulting
  to mandatory TLS outside development. Set `SMTP_USERNAME` / `SMTP_PASSWORD`
  for a hosted relay. If mail cannot leave the box, magic link — the only way
  back into a locked household — fails silently by design.
- The two production images have no wiring between them. `web/nginx.conf`
  hard-codes `proxy_pass http://api:8080`, so `hearth-web` cannot start alone.
- **The sign-up per-IP rate limiter's fix protects the production image only.**
  `web/nginx.conf` now sets `X-Real-IP` to `$remote_addr` and suppresses
  `True-Client-IP` on every API-proxying location, so a client can no longer
  spoof `chi`'s `middleware.RealIP` resolution through those headers there.
  But `docker-compose.yml` has no nginx service at all — in development, Vite
  proxies `/api` straight to `api:8080` with no header rewriting — so the
  per-IP limiter stays fully spoofable in development, and the pending browser
  walk (§1) cannot exercise the fix either way. The global daily mail ceiling
  (1000/day, reset at midnight, counted from `signups`) is what actually bounds
  the damage in the meantime.
- ~~**The production image cannot administer itself, and that is a lockout
  with no key.**~~ **Done.** `api/Dockerfile`'s `prod` target still has no
  shell, `goose` or `adminctl` — that stays deliberate, so a deployed `api`
  container carries nothing an intruder could use either. What changed is
  that it is no longer the only image: a third target, `admin`, on the same
  distroless base, carries both binaries, and `docker-compose.prod.yml` wires
  it in as a one-shot `migrate` service plus a `profiles: [manual]` `admin`
  service. So in production today: migrations run automatically ahead of
  `api`, and `unlock-household`, `reset-password`, `create-invite` and
  `prune` are all reachable via `docker compose run --rm admin ...` —
  documented end to end in `deploy/README.md`. The chain this used to
  describe (household-wide uncapped lockout, magic link as the only
  documented way back in, mail failing silently) is unchanged in its own
  right, but is no longer a dead end: `unlock-household --email` now exists
  as the second way back in. **Read "exists" narrowly.** The command is built
  and wired, and the `admin` image has been run against the live database
  (`goose status`, 2026-08-15) — but `unlock-household` itself has **never been
  run in production**. It is criterion 8 of the verification walk and that
  criterion is unrun, so this is a path believed to work, not one shown to.
  Anyone relying on it during an actual lockout would be trying it for the
  first time under pressure. Run it once on a calm day. `make reset-password` remains the *product's*
  only self-serve-adjacent recovery path in the sense the tracker's 🟡 on
  "Forgot?" describes — that gap is about the missing UI flow, not about
  whether an operator can reach the database.
- ~~**Putting any second proxy in front of nginx silently disables the per-IP
  sign-up limiter.**~~ **Configured, and now PROVEN on the live box
  (2026-08-15).** Criterion 10 of the verification walk exhausted the laptop's
  budget (five `202`s then a `429`) and then had a phone on mobile data accepted
  in the same window. nginx recorded `43.230.96.126` and `119.234.36.111` — two
  real client addresses, not Caddy's `172.28.x.x`. Mailpit corroborated
  independently: five laptop messages and one from the phone, with the sixth
  laptop request producing no mail at all, so the limiter fires before the send.
  Everything below stays true as a description of the trap for anything that
  *replaces* Caddy.

  **Putting any second proxy in front of nginx silently disables the per-IP
  sign-up limiter.** `web/nginx.conf` overwrites `X-Real-IP` with
  `$remote_addr` and strips `True-Client-IP` precisely so a client cannot spoof
  the limiter's key — and that comment assumes nginx is the edge. Put Caddy, a
  Cloudflare tunnel or a managed load balancer in front and `$remote_addr`
  becomes *the proxy's* address on every request, so `middleware.RealIP` keys
  every caller to one value and the per-IP limit becomes one global bucket.
  Per the rate-limit note above, a tripped global ceiling is silent and
  platform-wide. **Configured now, for Caddy**: `set_real_ip_from`,
  `real_ip_header X-Forwarded-For` and an explicit `real_ip_recursive off` are
  in `nginx.conf`, trusting the whole `172.28.0.0/16` compose subnet because
  Docker assigns Caddy's address from it (`docs/SYSTEM_DESIGN.md` §1 covers the
  trade-off). Anything that *replaces* Caddy as the thing talking to nginx
  needs its own address or CIDR added there and must write the true peer as
  the last `X-Forwarded-For` element. Anything put in front of *Caddy* is a
  different fix: `trusted_proxies` in `deploy/Caddyfile`.
- **Spoof-resistance has no permanent test home.** Forged `X-Forwarded-For` /
  `X-Real-IP` / `True-Client-IP` headers failing to open a fresh rate-limit
  bucket (the bullet above) was proven end to end through the real Caddy →
  nginx chain, but only by a throwaway script deleted once it passed. The
  deployment walk's own criterion 10 does not cover the gap: it exercises only
  the two-client property (two callers get two independent budgets), never the
  forged-header case. The Go suite cannot fill this in either — it tests
  `chi`'s `middleware.RealIP`, and the control being asserted lives in nginx's
  config, above the Go process entirely. So a regression in nginx's header
  rewriting would be caught by no test and no walk criterion today. Closing
  this needs either a scripted probe kept in the repo (not just run once and
  discarded) or a criterion added to the next verification walk that sends
  forged headers, not just two distinct source addresses.
- **The domain is the most fragile asset here — more fragile than the server.**
  `APP_BASE_URL` is embedded in every magic link and invite; SPF and DKIM bind
  to the domain; the cookie origin is the domain. A dead server is restored in
  an hour from the dump. A domain that lapses and is re-registered by someone
  else is gone permanently, and takes the only account-recovery path with it.
  Register long, auto-renew, and point the registrar's expiry notices at an
  address that is not hosted on that domain.
- **Succession is unanswered.** Over the forty-year horizon in
  `docs/adr/0001`, this holds a family's complete financial history on a VPS
  account and a registrar account in one person's name, behind a household-wide
  uncapped lockout whose only recovery is email. If that person is unavailable,
  nobody else can get in — not to the product, not to the box, not to the
  domain. This is a product question, not ops trivia, and it is the only item
  in this section that cannot be retrofitted after the fact.
- **A backup is not a backup until it has been restored.** Dumps in plain SQL
  rather than custom format (any future Postgres can load them, and a human can
  read them), at least one copy off the hosting provider entirely — a lapsed
  payment card takes the server and its snapshots together — and one restore
  actually performed and timed before it is needed for real.

### Assumptions with a long horizon, recorded rather than fixed

Neither of these is wrong today. Both are written down here because the
forty-year horizon in `docs/adr/0001` is what turns them from "fine" into
"someone will meet this and need to know it was a decision".

- **Argon2 parameters are frozen at the moment each password is set.**
  `ARGON2_TIME=3` and `ARGON2_MEMORY_KIB=65536` are appropriate for 2026 and
  will not be in twenty years. **The port makes a rehash-on-login impossible as
  written**, which is a sharper statement than "nobody implemented one":
  `usecase.PasswordHasher` is `Hash(plain) (string, error)` and
  `Verify(plain, encoded) bool`, so there is no channel to report that a stored
  hash's parameters differ from the configured ones.
  `Argon2Hasher.Verify` does parse `m`, `t` and `p` out of the stored
  string — it has to, to re-derive the key — and then discards them with the
  `bool`. Sign-in (`usecase/auth.go`) calls `Verify` and nothing else. So
  raising the configured cost protects only passwords set after the change, and
  every existing hash keeps its creation-time cost forever. The fix, whenever
  it is wanted, starts at the port rather than the adapter: widen the `Verify`
  result so a caller can learn "correct, but stored below the configured cost",
  then re-hash on that signal during a successful sign-in — the one moment the
  plaintext is in hand.
- **Money assumes two decimal places, everywhere.** `Money.String()` hard-codes
  them, which is why the ISO 4217 allowlist offers only two-minor-unit codes
  (see the tracker's Household settings section). A currency's minor unit can be
  redefined — Indonesian rupiah redenomination has been on the legislative
  agenda repeatedly, and this product is SGD/IDR by design — and the schema has
  no way to express "this amount predates the change". Deliberately not built
  for: the cost of carrying a per-row scale today is real and the event may
  never come.

---

## 6. Conventions worth keeping

These were each learned from a defect that shipped past a green test suite.

- **Fix the class, not the instance.** Fourteen times in this project a defect
  was fixed at one site while its siblings kept the bug — a PATCH corrected in
  two of three endpoints, an error oracle closed at the mailer and left two
  lines away, a non-awaited invalidation fixed in one panel with two untouched,
  a `time.Truncate`-and-location mistake that shipped at two separate call
  sites before a third, correctly-written one got its test, and (most recently)
  Bills' whole-branch review, where an archived-account refusal added by an
  earlier task in the same branch turned a rename into a dead end because the
  form beside it restated the field unconditionally. **`docs/LEARNING.md`
  pattern 1 is the list, one bullet per instance, and the count there is
  authoritative — recount it rather than copying this sentence.** When you fix
  something, grep for its shape.
- **Verify UI behaviour in a real browser.** jsdom's `<dialog>` is a stub, so
  five passing tests hid a modal that threw on every open in production. If a
  behaviour depends on the platform, a simulated DOM cannot tell you it works.
- **A test that cannot fail is not protecting anything.** Mutate it: break the
  code deliberately, confirm the test goes red, restore. This caught a sidebar
  ordering test whose fixture agreed with the wrong answer, and a guard test
  that asserted a transient state.
- **Ask what a caller can measure, not just what it is told.** Two enumeration
  oracles here were built from timing and from which deadline moved, not from
  error codes.
- **Pin tool versions.** Floating `@latest` broke this build twice.
- **Every 2xx except 204 carries a JSON body.** `apiFetch` throws on an ok
  response it cannot parse.

---

## 7. If you are handing this to an agent

The completed plans are the format that worked:
`docs/superpowers/plans/2026-07-26-hearth-skeleton.md`, `…-identity.md`, and
`2026-07-27-hearth-self-serve-signup.md`. All three were executed task-by-task
with a fresh implementer per task, an independent review after each, and fix
rounds until clean. Every task found a real defect in the plan — budget for
that rather than treating it as failure.

The most valuable review instruction was not "check this task" but **"what
sibling of this defect exists elsewhere?"**
