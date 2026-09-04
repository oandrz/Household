# Hearth — learning log

Every defect found while building slices 0, 1, self-serve sign-up and the
whole of slice 2 — Accounts, Transactions, Budget, Goals and Bills — plus the
finance-fixes round that followed the owner's first real day-one use of
Transactions, and the UX-repair round (M1) that followed his first look at the
app as a whole — and what each one teaches. That last round is worth noticing for what it
contains: not one of its defects was a broken function. Every one was
something correct in isolation that nobody had looked at as a product.
Written because almost none of them were caught by a failing test — they were
caught by someone asking the right question about code that looked fine and
had a green suite.

**Read the patterns first.** They are where the value is. The catalogue below
them is evidence, and a place to check when you touch that area.

**Add to this file when you finish a piece of work.** A defect nobody wrote down
gets rebuilt.

---

## The patterns

### 1. Fixing an instance rarely fixes the class

This happened **nineteen times** — one bullet each below, and the count is the
number of bullets, so recount it when you add one (it had already drifted by
one before the UX-repair round noticed). Almost every time, the fix was
correct and the sibling kept the bug; two of them are the variant where
nothing was broken at all until a field's or a product's meaning moved under
a reader nobody thought to look at, and one is the variant where an earlier
fix in the same branch *created* the sibling.

- `PATCH` implemented as `PUT` — fixed in `/household` and
  `/notification-preferences`, missed in `/household/members/:id`. Found two
  tasks later by someone building against it.
- A membership-oracle error path closed at the mailer, left in place at
  `NewToken` and `Create` **two lines below**.
- A non-awaited `invalidateQueries` fixed in `MembersPanel`, left in
  `CurrencyPanel` and `NotificationsPanel`.
- A pending-guard added to one control, not to its neighbour.
- `ErrInvalidMoney` mapped off 500 in the same round `fxRateMode` was left
  unvalidated and still 500ing.
- `time.Truncate` operates on the absolute instant, not on a calendar day in a
  particular location — and that misunderstanding shipped at more than one
  site on the accounts branch before anyone named it. An opening-balance
  "not in the future" check compared `asOf.Truncate(24 * time.Hour)` against
  `now.Truncate(24 * time.Hour)`, which refused *today's* date for part of
  every day east of UTC; fixed to allow a day of slack instead of truncating
  either side. The identical mistake then surfaced in the Postgres adapter's
  `dateOnly` helper, which turns `opening_balance_as_of` into a stored date:
  converting to UTC before truncating moved 07:00 on the 26th in Singapore
  back to the 25th. The frontend's own `today()` reads local calendar
  components on purpose and got the logic right the first time, but shipped
  with no test, on the stated belief that pinning it needed a
  `vitest.config.ts` timezone change — a reviewer disproved that belief
  (`process.env.TZ` already works in the test runner) and the test was added
  in the same task. **A fourth instance, seen live during the interim
  Overview's browser walk, on the server side and still open.** With the host
  at 1 August 06:24 (+08) and the API container in UTC — where it was still 31
  July — `/money/transactions` headed itself "0 in July 2026" while holding an
  August-dated transaction. That count comes from the server's own clock, so a
  household east of UTC sees last month's name over this month's ledger for
  the first eight hours of every month. The frontend side of the same walk was
  fine, and for a recorded reason: `currentMonth()` reads local calendar
  components, and `GET /budgets/{month}` takes its month from the URL rather
  than from a server "now", so Overview and Budget agreed on August with no
  clock involved. **The lesson is where the boundary sits.** Every one of these
  four is the same mistake, but the frontend ones were fixed by choosing local
  components and the server one cannot be — the server does not know the
  household's calendar. A derived "this month" computed server-side is a
  product decision (whose timezone?) wearing a bug's clothes, and it needs
  answering before it is coded, not after. Recorded against Transactions; not
  M2's to fix.

  **A fifth instance, found in review rather than live, and one step removed
  from `Truncate` itself: Bills' `domain.NextDue`/`startOfDay`.** Go's
  `time.Time.AddDate(0, 1, 0)` on 31 January returns 3 March, not 28
  February — it normalises "31 February" forward instead of refusing it —
  so a bill due on the 31st would walk off the end of every short month if
  the month arithmetic simply added one. The plan's own brief already named
  the fix (clamp the destination month's real length), but the first draft
  of `NextDue` and its sibling `startOfDay` read `from.Year()`/`.Month()`/
  `.Day()` straight off the caller's `time.Time` without converting to UTC
  first — the same family's essence restated: those three methods report
  components in the value's own `Location`, not UTC, so a bill built from a
  non-UTC input could compute its clamp against the wrong calendar day
  entirely. Task 2's review, following the plan's own constraint that month
  arithmetic must truncate in UTC and citing this very pattern, required
  `from = from.UTC()` in `NextDue` and `t = t.UTC()` in `startOfDay` before
  either reads a component; `TestNextDueAndIsOverdueNormaliseNonUTCInputToUTC`
  builds its inputs with `time.FixedZone` and was mutation-checked at both
  call sites independently. **The second-order lesson is why
  `due_anchor_day` is its own column rather than derived from `next_due`:
  clamping alone is one-way.** 31 Jan clamps to 28 Feb; if the next advance
  clamped from that already-clamped 28, it would give 28 Mar, and a bill due
  on the 31st would silently become a bill due on the 28th forever after its
  first February. Storing the anchor separately and clamping *it* fresh each
  time — 31 Jan → 28 Feb → 31 Mar — is what keeps the drift from compounding.

  **A sixth instance, in the layer above, and the reason to distrust a comment
  that says two things match.** `BillService.toView` computes `Overdue` through
  `domain.IsOverdue` → `domain.startOfDay`, which converts to UTC (the fix
  above). It computed `DueSoon` through the *package-local* `startOfDay` in
  `usecase/signup.go`, which deliberately keeps `t.Location()` because the
  signup rate limit resets at the household's own local midnight — correct
  there, wrong here. Two fields on one row, derived from the same two times
  under two different normalisations, with a comment asserting they were the
  same normalisation. `List`'s due-this-month probe read a raw `today` the same
  way. Latent rather than live, because `clock.System.Now()` returns UTC — but
  the false comment is what would have stopped the next person finding it.
  Fixed with a bills-local `billStartOfDay` whose comment says why signup.go's
  must not be reused, and pinned by a usecase-layer counterpart of the domain
  test. **A shared helper whose correctness depends on the caller's domain is
  not shared; it is two functions with one name.** When you reach for a
  package-local helper, read what its comment says it is for.

- Same slice, Task 15: `AccountModal`'s Balance field distinguishes "not a
  number" from "this currency doesn't use cents" (a switch to IDR/VND without
  touching the figure), with a comment explaining why restating the number
  back is the wrong message for the second case. `TransactionModal`'s first
  draft of Amount/Amount received had only the generic message, reachable the
  same way — logging an expense against the seeded household's own IDR
  account (BCA) with a figure that still has cents. Not caught by a failing
  test (none existed yet for this path in the new component); caught by a
  reviewer reading the sibling code before declaring the task done. Fixed by
  moving the distinction out of `AccountModal` into a shared
  `describeAmountError` helper in `formatMoney.ts`, used by both fields in
  both components, and adding a test plus a mutation check to each.

- Transactions slice, Task 9, found in the final whole-branch review.
  `usecase.AccountView.Balance` changed from a copy of the opening balance
  into a real SQL sum of the account's transactions. That is a change of
  *meaning*, not of shape: nothing failed to compile, no test went red, and
  every consumer kept reading the field it had always read. One of those
  consumers was `AccountModal.tsx` — a file no task on the branch owned — which
  prefilled its Balance input from `account.balance` and sent whatever was
  left there back as `openingBalanceMinor`. Because the gate on that resend is
  `balanceTouched || currencyTouched`, changing **only the Currency select**
  was enough: an account with a S$1,000 opening balance and S$300 of
  transactions since had its opening balance rewritten to S$1,300, Finances
  then read S$1,600, nothing on screen said anything had happened, and each
  such edit compounded the last. The modal's own comment had predicted this in
  full — it named the failure, named its precondition ("the same number today
  only because there is no transactions table yet"), and named the fix ("a
  touched edit will need this prefill to read from opening balance instead").
  The branch falsified the precondition and left the comment and the code
  exactly as they were. Nor could the fix have been local: `accountDTO`
  carried no opening-balance field at all, so there was nothing correct to
  prefill *from* until the read side was built. Fixed by putting
  `openingBalance` on the wire (redacted for a limited member alongside
  `balance`, since it is just as revealing), prefilling from it with no
  fallback to `balance`, relabelling the input "Starting balance" so it cannot
  be mistaken for the figure on the account row, and showing the current
  balance read-only beside it. `usecase/ports.go`'s doc comment still
  described the old world too, while `account_repo.go`'s comment — for the
  implementation of that same port — had been updated.

- `BudgetModal.tsx` had two category lists in play — `categories` (the
  active-only prop) and an archived-inclusive fetch already wired up for
  `addCategoryByName`'s restore-vs-create check — and `buildRows` read the
  wrong one. Reopening Edit budget for a line whose category had been
  archived since (the queue-archive flow saving it) rendered the row's
  fallback name, "Unknown category," instead of the real name with an
  archived marker; the page's own category grid, reading a different,
  correct source, never showed the bug. The sibling function in the same
  file had already solved "which list do I need" for its own purpose; the
  fix nobody had made yet was making the *other* function use it too. Found
  by Task 17's browser walk, not a test — every existing test's `categories`
  prop happened to already agree with its `includeArchived=true` stub, so
  the one case where they'd disagree (a category present in one, absent
  from the other) was never constructed. See the Frontend catalogue for the
  full fix, paired there with a second defect in the same modal (a
  duplicate-name guard that refused with zero feedback) found by the same
  walk.

- UX-repair round (M1), the audience copy. Every auth screen — sign-in,
  sign-up, the magic-link panel — described a product for **two people**:
  "where you both left off", "two owners", copy written when the imagined
  household was a couple. The domain had long since stopped modelling that:
  `domain.BuiltinSpaces` has a Family space for everyone, memberships carry
  limited members and children, Settings invites a family member with a role.
  Nothing broke; no test could go red, because the copy was exactly what its
  own tests asserted. This is the meaning-moved variant again, applied to the
  product's own description rather than to a struct field: the domain's
  readers include every rendered sentence about who the product is for, and
  none of them was swept when the domain grew past a couple. What would have
  caught it years earlier is embarrassingly cheap — read the front door and
  Settings **in the same sitting**, and ask whether they describe the same
  product.
- Same round, the sweep for that copy missed one and the browser walk caught
  it. Task 4 grepped for `two owners`, `two of you` and `both of you`, fixed
  every hit, and shipped. The sign-in screen still read "Sign in to pick up
  where **you both** left off." — a fourth phrasing of the same idea, matching
  none of the three patterns. The lesson is about the grep, not the decision:
  when the thing you are sweeping for is a *concept* rather than a token, it
  has no single shape, and a grep is only as complete as the list of phrasings
  you thought of. Either enumerate the phrasings deliberately (write them
  down, then ask what a writer would say that is not on the list), or grep for
  something structural instead — here, every copy constant in the auth
  feature, read end to end, would have been a shorter list than the phrasings.
  Found by opening the sign-in screen in a real browser, which is the backstop
  when the grep is the thing that is wrong (fixed in `8fe1f1a`; see
  `docs/superpowers/plans/2026-07-31-hearth-ux-repair-verification.md`
  criterion 15).

- **Goals fixed "a limited member's routine 403 needs its own explanation,
  not the generic alert" for Goals alone, and nobody swept for the class —
  Bills' Task 18 walk found it recurring in two more places at once.**
  `GET /goals` is money-AND-owner gated, and `GoalsPage.tsx` distinguishes
  that 403 from a genuine failure with its own `goals-owner-only` branch.
  `router.go`'s own comment says the identical guard covers the whole `txn`
  group — transactions, categories, budgets, goals, bills — not goals alone.
  `BillsPage.tsx` had never been given the branch (criterion 14 of the
  walk's own brief found it: a limited member holding `money`, following
  nothing more alarming than the sidebar's own Bills link, landed on the
  same red `bills-load-error` alert a database outage would produce).
  Fixing that one instance and re-reading `router.go`'s own comment — which
  names the whole group in one sentence — was what prompted checking the
  other three pages built against the same guard rather than closing the
  task on Bills alone: `BudgetPage.tsx` had the identical gap, one line
  worse (`if (budget.error || !budget.data)` collapsed the 403 and the
  post-`TanStack`-contract type-guard into the same generic alert, so
  splitting them was part of the fix); `TransactionsPage.tsx` had it too,
  with no test in either direction, same as Bills' own gap. Neither page
  had so much as an absence test to have been fooled by — the branch had
  simply never been written, in three different files, by three different
  tasks, none of which had reason to know a sibling page existed with the
  same guard. All three fixed in the same mirror shape `GoalsPage.tsx`
  already used, each pinned by its own two-test pair (403 renders the
  explanation and not the generic alert; a 500 renders the generic alert
  and not the explanation) and mutation-checked independently. **A comment
  naming "the whole group" in one file is not the same as every reader of
  that group having read it** — `router.go`'s own sentence was true and
  specific the entire time; nothing made the three frontend pages built
  against it go and act on it. **RetrosPage.tsx (task 10, `docs/superpowers/sdd/2026-08-16-hearth-retros/`) is the first page since that sweep built against this exact guard from a blank file, and it was given the branch on day one rather than found missing it later** — `GET /retros` is marriage-AND-owner gated the identical shape (`router.go`'s own comment on the group: `requireOwner` is stacked even though a limited member can never hold `CapMarriage` today, precisely so the frontend guard is not leaning on an invariant enforced only one layer down), so the brief named `GoalsPage.tsx`'s branch as the shape to mirror exactly and required the same mutation check (collapse the 403 branch into the generic one, confirm the owner-only test alone goes red and the 500 test stays green) before calling it done. Evidence for this pattern's own point either way: a class of bug recurring three times is what got this written down as a thing to check for on the *next* page, not just the next fix.

- **A fix created the very sibling it was meant to close — inside the same
  branch.** Bills' Task 10 added an archived-account refusal to
  `BillService.Update`, correct on its own and the mirror of the one `Create`
  already had. But `BillModal`'s edit body put `payFromAccountId` in *every*
  PATCH, so from that commit onward a bill whose pay-from account had since
  been archived could not be renamed at all: the save came back "That account
  is archived and cannot be used to pay a bill," for an edit that never touched
  the account. The currency check runs first, so a household whose archived
  account was its only one in that currency could never edit that bill again.
  The service change and the form that feeds it were reviewed in different
  tasks, and each was right about its own file. **When you add a refusal, grep
  for who already sends that field unconditionally** — a new guard's blast
  radius is every caller that restates a value it did not change, which is the
  same "don't restate a field the form didn't touch" habit this pattern already
  teaches for derived figures. Fixed by sending the field only when it differs;
  the class turned out to have exactly one member (`Account.IsArchived()` is
  checked on a write path in `bill.go` and nowhere else in `usecase`, so
  `TransactionModal` restating its own account ids is harmless), and confirming
  that was part of the fix.

- **The class can be an invariant rather than a line of code.**
  `TransactionService.Create` enforces four write-invariants on anything
  entering the ledger: accounts, amount, payer, category. `BillService` — the
  ledger's second front door, because `MarkPaid` writes a real `transactions`
  row — re-implemented three of them and omitted the category one, so
  `POST /bills` accepted an **income** category id and the resulting expense
  landed in Budget's `Spent` but in no category row at all
  (`buildCategoryViews` walks expense categories only). Task 8 of the same
  branch had *already* fixed the payer axis after exactly this reasoning; no
  one then asked which other axes the same argument covered. **When you find
  one invariant missing at a new door, enumerate the whole set at the old door
  and check each one** rather than fixing the axis the report happened to name.
  The port to depend on already existed (`CategoryLookup`) and the repository
  already satisfied it — the omission was never a design constraint.

- **A grep-based inventory finds only the spelling it was written for.** The
  mobile-responsive plan's own touch-target task built its Step 1 inventory from
  `grep -rEn 'h-6 w-6|h-7 w-7|py-1\.5'` and fixed every match to a 44px floor —
  correctly, and the review approved it. But `Sidebar.tsx`'s `NAV_ITEM_CLASS`
  reads `px-2.5 py-2`, which that pattern cannot match, so all seven of the
  drawer's own navigation links — the primary touch target of the entire mobile
  experience — shipped at 36.3px, invisible to the sweep that was supposed to
  catch exactly this. A follow-up audit measured every control in a real
  browser instead of grepping for a padding value, and found the same gap
  repeated at roughly twenty more sites across the app: any control whose class
  already read `py-2` or `py-2.5` (not `py-1.5`) was equally invisible to the
  original grep, from `BudgetPage.tsx`'s History/Edit budget buttons to every
  Cancel/Save footer and every labeled field in `AccountModal.tsx`,
  `BillModal.tsx`, `GoalModal.tsx` and `TransactionModal.tsx`. **A grep-based
  inventory is a hypothesis about how the codebase spells the thing you're
  looking for, not a census of it** — the task's own risk paragraph had already
  named "nav rows" as a category to check, and the grep dropped it anyway,
  because grep can only find the string you wrote, never the ones a sibling
  file wrote differently. The fix that actually closed the gap used the brief's
  own stated intent instead: drive the page in a real browser and measure
  `getBoundingClientRect()` on every interactive element, which finds a 36.3px
  link the same way regardless of which Tailwind class produced it.

- **UI-polish round: naming instances found half the defects; writing the rule
  down found the rest.** Measured first: `tabular-nums` on the Overview hero
  figure — a lone display figure with nothing else on screen for it to align
  against — cost 196.81px against 172.84px without it, 24px wider, 14%, the
  extra width landing as gaps around the comma and the decimal point rather
  than anything a reader gains. `tabular-nums` only earns its width where a
  column of figures needs to line up; a figure with nothing beside it never
  has that column. The first correction, made from that one measurement,
  named two specific sites to fix. Review of the same change found two more.
  Only once the *rule itself* was written down — never on a lone figure, only
  where digits actually stack — and a sweep was asked for against that rule
  did the remaining four surface: eight sites in total, so naming instances
  alone would have missed six of the eight. The rule now lives once, at its
  one canonical site — the `.tabular` comment in `web/src/index.css` — rather
  than being restated at each of the eight call sites that follow it.

**When you fix something, grep for its shape before you close it.** The question
that finds these is not "is this fixed?" but "where else does this pattern
appear?" `Truncate` is now that grep for date-and-location bugs specifically —
run it before adding a fourth site.

**And a concept has no single shape, so a grep for one is a guess at a list.**
Code has tokens you can search exactly; an *idea* — "copy that assumes two
people", "places we format a currency" — is spelled several ways by several
authors, and the one nobody listed is the one that survives. Write the
phrasings down before you sweep, add the ones you would not have written
yourself, and treat the sweep as unproven until something that does not read
your grep — a browser walk, a reviewer, a rendered-bundle search — has looked
at the result.

**And three copies of a thing share nothing but a coincidence.** Before the
interim Overview, `AccountModal`, `TransactionsPage` and `MembersPanel` each
declared a byte-identical `fetchHouseholdMembers` + `useHouseholdMembers` pair
against the same `["household", "members"]` key. Nothing was broken — they
shared one cache entry, which is what each of their comments said they wanted.
But they shared it *by coincidence*, and the coincidence was one keystroke
deep: any of the three could have changed its key, kept passing its own tests,
and silently split the cache while `MembersPanel`'s mutations went on
invalidating a literal only two of them still used. It surfaced only when a
fourth caller needed the same data. **A comment saying "this is deliberately a
copy of that" is a design decision that expires** — at two it is a judgement
call, at three it is a shared thing nobody owns. Grep for the endpoint before
writing a hook for it.

- **The fix for a lying label left the same lie one row below it, and only a
  browser found it.** The Transactions screen's header claimed a month its
  ledger did not describe — "0 in August 2026" over ten July rows — and the
  fix made the default month apply to the list and the summary together.
  Twelve tests, three mutation checks, both suites green. Then the M2 browser
  walk picked July in the filter and read: header "10 in July 2026", ledger
  full of July rows, and directly beneath them **"Spent this month
  S$10,872.09"** — the identical claim-does-not-describe-the-figure defect,
  in a second label on the same screen, untouched because the work had been
  framed as "the header and the list" and that label was neither. It had been
  wrong before this round too; the round simply made it reachable in one
  click by putting a populated month control on the screen. **When you fix a
  label that lies, the class is every other label on that screen, not the one
  the bug report named** — and the instrument that found it was driving the
  real page, not any test that could have been written for the fix as scoped.

- **Giving a month-less request a meaning changed what every other caller of
  that endpoint was asking for.** Fixing the Transactions month contract made
  an absent `month` mean the current month. `GET /api/v1/transactions` has a
  second caller: `RecentTransactionsCard`, the "Recent transactions" strip on
  `/money`, which passed `{}` and whose own header comment said so proudly —
  it shared one cache entry with the ledger's default request precisely
  *because* the two were the same question. They stopped being the same
  question in the commit that gave the default a meaning, and the card
  silently became "recent transactions this month": on this dev household it
  went from five rows to one, and on the first of any month it would show a
  household with years of history nothing at all. That is the same defect
  shape the round had just fixed, reintroduced one file over by the fix.
  Caught by the caller sweep, then reproduced in the browser; **no test on
  either side could have caught it**, because `stubFetchRoutes` matches exact
  URLs and a stub is blind by construction to the *server's* meaning of a
  request moving underneath it — worse, the card returns `null` on a query
  error, so a wrong URL makes the stub throw into a component that renders
  nothing and a suite that stays green. **When you give a previously-neutral
  default a meaning, grep every caller of that endpoint in the same change**:
  a default is a value with readers, exactly like a field whose meaning moved.
- **The same file, the same defect, a third time — and the two earlier fixes
  were written down at the line and still did not stop it.**
  `SignInScreen.tsx` already carried two comments naming this exact class:
  "Fix round 2, Finding 1" (a magic-link error surviving the move into the
  sent panel) and "Fix round 3, Finding 1" (the same thing again). Telegram's
  "Continue with Telegram" button added a third instance the same way — it
  cleared its error banner on success only, so a stale error, and a stale
  popup-blocked link, both kept showing under a control that had just worked.
  What makes this worth a bullet rather than a shrug is that **the two prior
  fixes were documented, specific, and adjacent, and the new control still
  reproduced them.** A comment at the site of a past fix does not protect the
  *next* control added to the same screen; nobody reads a neighbour's comment
  before writing a new `useState`. The habit that would have caught it is
  mechanical, not attentive: **when you add a per-attempt banner to a screen,
  grep that file for every other `setXError(null)` and match the moment they
  fire** — here, "at the start of every attempt", not "on success".

**And when you change what a value *means*, its readers are the class.** The
compiler will not find them, because nothing about the type changed; only a
grep for every reader will. Two habits fall out of this one. Sweep the
consumers in the same change that moves the meaning, including the ones in
files your task does not own — the producer and its readers are one change,
not two. And treat a comment that says "this is only safe until X" as a debt
that comes due: whoever ships X owns it. Here the comment was right, specific,
and load-bearing, and it still did not stop the defect, because nothing made
shipping Transactions go and read it.

**The cheapest sweep is a grep for the literal string you are about to
change, run before you change it.** M3's Bills panel turned out to share a
byte-identical class string with `BudgetPage`'s `budget-insight`: same shape,
same missing border, same position last in a right rail under a bordered card.
The grep found it in seconds and both were fixed in one commit. Had it gone
the usual way, the fix would have closed one disagreement and left an
identical one a page over, for the next browser walk to file as a fresh
defect. (No bullet added above: this one was caught before it shipped, and the
count stays the number of bullets.)

### 2. A test that cannot fail protects nothing

- A sidebar ordering test supplied spaces **already in ascending order**, so a
  component that re-sorted would have passed identically — and re-sorting was
  the exact bug it existed to prevent.
- `TestUsersWithoutAPasswordCannotSignIn` passed with the guard deleted, because
  the fake hasher happened to reject an empty hash for its own reasons.
- A capability filter had no test that would notice its deletion.
- `BillService.Update`'s payer check could have its two arguments **swapped**
  and every test still passed: the double computes
  `memberships[membershipID] == householdID`, which is false for a swapped
  pair too, so the refusal test was satisfied either way. `Create`'s identical
  call *was* pinned, by a happy-path test that set a valid payer. A refusal
  test alone never fixes the argument order — only a case that must SUCCEED
  can. Check every guard for whether its happy path is tested, not just its
  refusal.
- `MostRecentBillPaymentDueOn`'s `bill_id = $2` filter could be dropped with
  the whole suite green, because no test ever gave one household two bills
  that both carried payments. In production that refuses a legitimate undo on
  bill A because bill B has a later payment. **A scoping clause needs a
  fixture with something for it to exclude**; a one-row fixture tests the
  query minus its WHERE.
- `defer tx.Rollback(ctx)` in `BillRepo.UndoPayment` could be deleted with the
  whole suite green, and — this is the trap — the obvious test for it stays
  green too. Removing the rollback does not make the partial writes visible:
  nothing commits them either, so "assert the rows survived" passes both ways.
  What actually breaks is that the transaction is never *ended*: its
  connection never returns to the pool and its row locks are never released.
  The assertion that catches it is `pool.Stat().AcquiredConns() == 0`. **Before
  writing a test for a cleanup call, work out what observably differs when it
  is missing** — for rollback-on-error that is usually resource release, not
  data.
- Five of seven invite tests used a fetch stub that **matched positionally and
  ignored the URL** — they would have passed while the component called a
  different endpoint entirely.
- `-run Lock` matched two of six lockout tests, so "six tests pass" was never
  what was verified.
- A task brief's quoted test filters (`-run TestHousehold`, `-run TestInvite`)
  matched **zero** tests in this repo — `go test` reports "no tests to run" for
  a filter that matches nothing, and exits zero. A filter copied from a brief
  is not evidence anything ran; run the whole package.
- The owner-gated route-walk matrix (`TestOwnerOnlyRoutesRejectALimitedMember`)
  would have refused every accounts write route at `requireCapability` and
  never reached `requireOwner`, because the existing limited-member fixture
  holds no `money` capability — a vacuous green, spotted while writing the
  accounts spec rather than by a failing test. The fixture gained a second
  limited member who does hold `money`, so the walk's caller can fail on
  `requireOwner` specifically instead of being turned away one guard earlier.
  A matrix proves nothing unless the caller can get past every guard except
  the one under test.
- Deleting `NetWorthSummary`'s domain-ordered breakdown loop (replacing it with
  a bare range over the `byType` map) left every test green across eight fresh
  processes, so the rule that two identical requests must not reshuffle the
  chart had nothing behind it. A five-type ordering test, where map iteration
  lands on the sorted order by chance only one time in 120, is what actually
  discriminates.
- Two `FinancesPage` test assertions used a synchronous `getByText` inside an
  already-awaited card, so they never actually waited on the `useCurrencies` /
  `useMe` queries their own subject depended on — both passed only because,
  in practice, every mocked fetch in that test happened to settle within the
  same microtask batch. A reviewer proved it by adding a 20ms delay to one
  unrelated stubbed route: both assertions broke, deterministically, on a
  change to code neither of them exercises.
- Two separate mutation checks in this plan did not discriminate on the first
  attempt. One (validate a patch built only from its non-nil fields, instead
  of the merged account) made three tests fail, but the one the check was
  meant to pin failed for the *wrong* reason (`ErrAccountNicknameRequired`,
  because an isolated `Type: &loan` patch has no nickname) rather than the
  reason it claims to catch (`ErrLiabilityBalanceNegative`) — broad,
  wrong-reason failure proves a mutation breaks *something*, not the specific
  thing it names. The other (`Math.round` in place of a float-safe parse) left
  the *named* test green outright, because `Math.round` happens to repair the
  exact float error that test exists to catch, and a different, unrelated test
  failed as collateral damage instead. Both implementers noticed on their own,
  devised a more surgical mutation that isolated the one claim in question,
  and reported both attempts rather than only the one that worked.
- `VisionModal`'s mode-switch test (Vision spec's task 12) asserted the
  "switching clears the other mode's inputs" rule by reading the DOM after a
  typed → linked → typed → linked round trip: current/target absent while
  linked, a blank goal picker on the first visit, current/target back at
  their defaults after returning to typed. The mutation the task's own Step 5
  names (`setMeasureMode`'s typed branch no longer clearing `goalId`) left
  every one of those green. The reason is structural, not a weak assertion:
  the *linked* branch clears `goalId` unconditionally on every entry,
  regardless of what the typed branch just did to it, so re-entering linked
  a second time always shows an empty picker whether or not the bug is
  present — the round trip's own last step erases the evidence of the first
  leg's mistake. And typed mode renders no goal field at all, so there is
  nothing in that state's DOM to read the leaked value back from either. The
  only place a preserved `goalId` is observable is the wire: the test now
  saves while still in typed mode and asserts the exact PUT body carries
  `goalId: ""`, which the mutation turns red immediately. **A "cleared"
  claim about a field one render state hides is not tested by any assertion
  confined to states that render it** — if every path back to a check point
  passes through a step that resets the very thing being verified, only the
  data that actually leaves the component (the request body, not the DOM)
  can still tell the two branches apart.
- Two mutation-check gaps caught before shipping, both in `VisionCard.tsx`
  (Vision spec's task 13), by a reviewer reading the draft test file before
  either mutation was actually run. First: the "renders nothing at all for a
  year with no vision" test's fixture was going to be an empty vision
  (`theme: ""`, `pillars: []`) at `version: 0` — the same shape the guard is
  *supposed* to produce. A mutation that deleted the `version === 0` check
  entirely would have rendered exactly that same nothing, for a completely
  different reason, and the test would have stayed green either way, never
  discriminating the guard from its absence. Rewritten to build the fixture
  from otherwise real, renderable content (a real theme, a pillar with a
  measure) with only `version: 0` set — now deleting the guard makes real
  content appear, and the test goes red for the actual reason it claims to
  check, exactly like the `real_ip_recursive` case above: **a "renders
  nothing" test proves nothing about the guard unless there is something
  real for the guard to be suppressing.** Second: the "shows the pillar's
  FIRST measure" test's two-measure fixture originally gave both measures
  the same label ("Date nights / month") with only their figures differing.
  A mutation reading `measures[measures.length - 1]` instead of
  `measures[0]` would have shown a different figure but the *same* label
  text, which the test's only assertion (`toHaveTextContent(label)`) cannot
  see — a presence check on a label two measures share is blind to which of
  them actually rendered. Given the two measures distinct labels **and**
  distinct figures, plus explicit `not.toHaveTextContent(...)` assertions on
  both halves of the second measure, and the `measures.length - 1` mutation
  now goes red immediately. **When a "the first N" or "the last N" rule is
  being tested, the fixture's other N-1 entries need to differ from the one
  under test in every field the assertion reads — a shared value in any one
  of them is a mutation the test cannot see.**
- **The same feature produced three more instances of this pattern, each
  claiming more than its assertion checked.** First, Vision spec's Task 1:
  `TestVisionMeasureCannotBeBothTypedAndLinked` originally asserted only
  `err == nil` after inserting a measure carrying both a goal and a typed
  target — but the test's own name claims `measure_is_typed_or_linked`
  fired specifically, and a bare `err == nil` check passes identically for
  a typo'd column name or a dropped connection. Fixed by unwrapping to
  `*pgconn.PgError` and asserting `ConstraintName ==
  "measure_is_typed_or_linked"` by name, so the test proves the database
  refused *this* ambiguous row, not merely that something went wrong.
  Second, Task 4: `TestVisionRepoGetReadsPillarsMeasuresAndMilestonesInPositionOrder`
  inserted a single pillar, so `ListVisionPillars`' `ORDER BY position`
  could be deleted with nothing going red — a one-row fixture cannot prove
  an ordering clause, the identical shape pattern 2's own `bill_id` entry
  above already names for a `WHERE` clause. Fixed by inserting a second
  pillar out of position order and asserting `Pillars[0]` is the
  position-0 one. Third, Task 10: the frontend's two `useVision` save tests
  asserted the outgoing `PUT` body with `toMatchObject({ version })` —
  which passes for *any* body that happens to carry that one field, so a
  save that silently dropped `theme`, `description`, every pillar and
  every milestone from the request would have gone unnoticed. Fixed by
  widening both to `toEqual(the full expected body)`. All three were
  caught in review, by rereading what each test's name and its assertion
  actually proved against each other, not by a mutation run that had
  already gone red — the same reading the two entries above this one
  needed.
- A comment on `FinancesPage.test.tsx`'s archive-toggle test claimed to be
  "the end-to-end proof" that `invalidateAccounts` (`useAccounts.ts`) returns
  its `Promise.all` rather than firing it and forgetting. Disproven: dropping
  the `return` left that test, and six siblings in the same file, green.
  Two TanStack Query / Testing Library facts combine to make this the
  general shape to watch for, not just an accounts-specific slip:
  `queryClient.invalidateQueries` dispatches its refetch whether or not the
  caller awaits the promise it returns, and `await findByText(...)` polls
  the rendered DOM, not mutation state — so a test built on "the list
  eventually shows the right thing" cannot tell "settled because the
  refetch actually landed" apart from "settled because nobody waited for
  it." What the `return` actually gates is `isPending` (here, the
  Archive/Restore button's `disabled` prop), and only a test that asserts
  on *that* — holding the refetch open with a deferred promise, per
  `SignInScreen.test.tsx`'s existing pattern — can tell the two apart.
  Corrected to describe only what it proves, with a second test added
  alongside it that asserts the disabled state directly.
- `TestEnsureSeededSurvivesConcurrentFirstRequests` (category seeding, Task 5)
  fired eight bare `go func(i) { ... }(i)` goroutines against `EnsureSeeded`
  and stayed green, five runs straight, with the `ON CONFLICT DO NOTHING`
  deliberately removed from `SeedCategories`. The pool in `postgres.Open`
  dials only one connection up front (`Ping`); each goroutine's first query
  pays its own connection-dial latency, which was enough to serialise the
  count-then-insert window the test exists to race. Warming the pool with
  sixteen concurrent `List` calls first, then releasing every seeding
  goroutine through the same closed channel, reproduced the race reliably —
  the same code, unmutated, now fails 15/16 with
  `constraint "categories_household_id_name_key": already exists` the moment
  `ON CONFLICT` is removed, and is stably green five runs in a row restored.
  A concurrency test that starts its goroutines with a bare loop is only
  proven to race if the work per goroutine already dwarfs connection setup;
  otherwise warm the pool and use a start barrier before trusting it.
- Same task, a second instance, found by review after the first was already
  fixed: `TestEnsureSeededDoesNotRebuildOverArchivedCategories`'s comment
  credited the archived-row protection to `SeedCategories`' unique key and
  `ON CONFLICT`. It does not reach either. `CountCategories` has no
  `archived_at` filter, so a household with all thirteen categories archived
  still counts as thirteen; `EnsureSeeded`'s `if count > 0 { return nil }`
  fires before the second `EnsureSeeded` call in that test ever calls
  `SeedCategories`. The test still pins something real — a cleared list stays
  cleared — but the *reason* in the comment was wrong, and it would have
  passed identically with `ON CONFLICT` deleted outright, same as the
  concurrency test above. Fixed by correcting the comment to name the count
  check, and by adding
  `TestSeedCategoriesRespectsTheUniqueKeyEvenWhenEveryRowIsArchived`, which
  calls the generated query directly — bypassing `EnsureSeeded`'s count check
  — against an already-archived household, and does fail with a duplicate-key
  error the moment `ON CONFLICT` is removed. Two short-circuit-shadowed tests
  in one task, one caught only by review after the first was already
  believed fixed, is the point of this section: a passing mutation check on
  a *different* test in the same file is not evidence for this one.
- Same slice, Task 7: `TestDeletingAMemberKeepsTheirTransactionsAndDeletingAHouseholdTakesThemAway`'s
  final assertion — `DELETE FROM households` must succeed — carried the
  comment "The cascade RESTRICT would have blocked," claiming to prove
  `transactions.from_account_id`/`to_account_id` are `ON DELETE CASCADE` and
  not `RESTRICT`. Mutating both columns to `RESTRICT` and rerunning left the
  test green. The confound is one level less obvious than the other entries
  here, because the test *did* delete something and the delete *did*
  succeed — it looked exactly like the proof it claimed to be.
  `transactions.household_id` is itself `ON DELETE CASCADE` from
  `households`, so deleting the household fires two independent RI cascades
  at once: one deletes the household's accounts, the other deletes the
  household's transactions directly via `household_id`. In this environment
  the `household_id` cascade ran first, removing the transaction row before
  the accounts cascade ever reached the account it referenced — so the
  (mutated) `RESTRICT` on `from_account_id`/`to_account_id` had nothing left
  to restrict against by the time it was checked, and passed trivially. A
  household-level delete can prove `households` cascades to `transactions`
  end-to-end; it cannot, by itself, prove anything about which FK action a
  *different*, indirectly-cascaded column carries, whenever another path to
  the same row's deletion exists. What would have caught it sooner: asking
  "does this test exercise the column under test directly, or only as a
  side effect of a bigger deletion with its own path to the same result?"
  before trusting a comment that names a specific constraint. Fixed by
  adding `TestDeletingAnAccountTakesItsTransactionsWithIt`, which deletes an
  account directly — the household and the referencing transaction both
  still otherwise present — for both `from_account_id` and `to_account_id`;
  that is exactly the shape `RESTRICT` would block, with no second cascade
  path to confound it. The original test's comment was corrected to say
  what it actually proves rather than deleted, since the household-cascade
  property it does prove is still real coverage.

- Same slice, Task 8: the brief's own
  `TestPagingIsStableWhenARowIsInsertedMidScroll` created ten transactions on
  ten *different* days, then paged with a keyset cursor. A cursor comparing
  only the date and one comparing the `(date, id)` pair return identical
  results whenever no two rows share a date — there is never a tie for them to
  disagree over — so the brief's own mutation instruction (weaken the
  predicate to date-only, confirm red) would have reported a false pass
  against its own fixture. Caught before ever running the mutation, by tracing
  the parent task's explicit question rather than trusting the brief's test as
  given. Fixed two things together, because the assertions had the same gap
  as the fixture: added a second transaction dated onto the page boundary
  (deterministic given the loop bounds and page size, not random — with days
  11–20 and a page of 4, the boundary row is always the 17th), and added an
  explicit "the boundary's sibling row must appear on page two" assertion.
  That second part mattered on its own: the existing assertions (no id on
  both pages, nothing newer than the cursor) do not notice a row that simply
  vanishes, and a date-only predicate's actual failure mode here is silently
  dropping the whole boundary date, not duplicating a row — a duplicate-only
  check would have stayed green even with the fixture fixed. Confirmed the
  mutation red (`page two is missing ..., the cursor's own sibling on 17
  July`) before restoring.

- Same slice, Task 11: `TestSpendCountsATransactionDatedBeforeTheAccountsOpeningBalance`
  (`internal/usecase/monthsummary_test.go`) asserts that a transaction dated
  before its account's opening balance still counts toward the month's spend
  — the split decision 6 pins: excluded from the balance, still counted as
  spend, because the money was actually spent. `MonthSummary` decides that by
  reading `TransactionView.BeforeFromAccountOpening`, but neither test double
  the brief supplied (`fakeAccountLookup`, `fakeTransactionRepo`) ever set
  that field anywhere — it is populated only by the real Postgres join in
  `adapter/postgres/transaction_repo.go`. The flag was permanently `nil` in
  every run of this test, so a `MonthSummary` that actively filtered out
  `BeforeFromAccountOpening` rows — the exact opposite of the rule being
  tested — would have seen nothing to filter and passed identically;
  confirmed by injecting exactly that filter and watching the test stay
  green. Fixed by giving `fakeTransactionRepo` a
  `markBeforeFromAccountOpening` setter, called on the transaction under test
  before `MonthSummary` runs — the double now actually simulates the
  condition its own name claims to test, and the same injected filter now
  fails for the right reason (`spent = 0, want 840`).

- Same slice, Task 13: `TestTransactionRoutesRequireMoneyAndOwner`
  (`internal/adapter/http/api_test.go`) walks the new transactions and
  categories routes to prove each requires `money` and owner. The test
  harness builds its own `httpadapter.Deps` in `newTestEnvWithClock`, entirely
  separate from `main.go`'s wiring, and the brief's own instructions covered
  wiring `Transactions`/`Categories` into `main.go` only — never into the
  harness's copy. With those fields left nil, every owner request panicked on
  a nil pointer inside the handler and was recovered by `recoverer` into a
  bare `500`. The test's own assertion was `rec.Code != 401 && rec.Code !=
  403` — true of *any* status that isn't one of those two, 500 included — so
  three of five routes 500'd on a dependency that was never wired, and the
  test passed as if it had proven the guard. Found by reading debug logging,
  not by the assertion going red. Fixed by wiring the harness's `Deps` the
  same way `main.go` is, and replacing the two-way exclusion with an exact
  expected status per route, so a route whose handler never ran fails loudly
  instead of sliding through a check built to reject only two specific
  numbers.

- Same slice, Task 15 (the Log-a-transaction modal): the brief's given test
  file imported `userEvent` from `@testing-library/user-event`, which is not a
  dependency of this project at all — only `@testing-library/react` and
  `jest-dom` are installed, and every other modal test here (`AccountModal`,
  `AccountsPanel`) already uses `fireEvent`. Run as given, the whole file would
  have failed to import, not failed one assertion — caught before ever running
  it, by trying to resolve the same failing-for-the-right-reason RED step this
  checklist requires and getting a module-resolution error instead of a test
  failure. Rewritten to use `fireEvent`, matching the rest of the codebase.
  Separately, the same brief's account fixtures were the flattened
  `{ id, nickname, currency }` guess `AccountsPanel.test.tsx`'s own comment
  above the brief's code already warned about ("follow whatever shape ... this
  project already builds its account fixtures with") — `schemas.ts`'s real
  `Account` nests currency under `balance` — and its received-amount test
  selected `"ocbc"` as the same-currency destination without that account
  existing anywhere in the fixture list. Selecting a nonexistent `<option>` is
  a silent no-op in both jsdom and a real browser, so the "optional within one
  currency" half of that test would have run against whatever the destination
  select's default already was, never actually exercising a same-currency
  selection distinct from the later cross-currency one. Fixed by building full
  `Account` fixtures (a small `account()` helper supplying every required
  field) and adding `ocbc` as a real, second SGD account. A third gap had no
  test at all in the brief: nothing checked that the Category dropdown
  actually filters by kind, and the brief's own fixture only ever names one
  category, which would make that filter untestable even if a test existed —
  "no income categories shown for an expense" is true whether or not the
  component filters, when income categories were never in the list to begin
  with. Added a category fixture with both kinds and a dedicated test,
  confirmed red by temporarily returning every category regardless of kind.

- Same slice, Task 16 (the Transactions page): the brief's given test file
  imported `userEvent` from `@testing-library/user-event` again — the exact
  same wrong dependency Task 15's brief sketch used, in a different task's
  brief, despite Task 15's own report already naming the gap and every real
  modal test in this codebase already using `fireEvent`. Rewritten the same
  way. The brief's `renderPage`, `expenseFixture` and `patched`/`deleted`
  spies were also, as its own task instructions warned, guesses at shapes
  that did not exist yet: `patched`/`deleted` needed building against
  `stubFetchRoutes`'s real `capture` hook (parsed request body in, an
  arbitrary call signature out), not assumed into existence. The mutation
  check named in the brief (drop the "are filters set" condition, confirm
  "distinguishes an empty ledger from filters that match nothing" goes red)
  passed on the first try here — evidence that a brief's own suggested
  mutation is not automatically suspect, only that it has to be checked
  every time rather than assumed correct from the fact that it was written
  down. Two further gaps survived every mutation check run before an advisor
  pass caught them, both hiding behind an assertion that fired without
  constraining enough to matter. First: the delete test's DELETE stub was
  `{status: 204, body: null}` — `JSON.stringify(null)` is the four-character
  string `"null"`, and `stubFetchRoutes` builds `new Response(...)` from
  exactly that, which the Fetch spec's own `Response` constructor refuses to
  do for a 204 (confirmed directly: `new Response(JSON.stringify(null),
  {status: 204})` throws `TypeError: Invalid response status code 204`;
  swapping in `body: undefined` — `JSON.stringify(undefined)` is
  `undefined`, a legal null body — does not). Because `stubFetchRoutes`'s
  `capture` hook records the request *before* constructing that `Response`,
  the test's only assertion (the `deleted` spy) was satisfied even though
  the fetch then threw, the mutation rejected, and `TransactionModal`'s own
  `.catch` left the dialog open on a submit error the test never looked for.
  No other test in this codebase had stubbed a 204 before, so this exact
  interaction — `stubFetchRoutes` plus a null-body status — was untried
  territory. Fixed the stub (`body: undefined`) and added a permanent
  `expect(screen.queryByRole("dialog")).not.toBeInTheDocument()` assertion,
  since a spy recorded before a throw cannot tell a completed action from a
  merely-attempted one. Second: the edit test's PATCH-body assertion used
  `expect.objectContaining({ description: ... })` — true of a request that
  also happened to carry a wrong `categoryId` or a hardcoded
  `clearReceivedAmount: false`, which is exactly the pointer-semantics
  translation this task's own headline requirement exists to get right (see
  the Frontend catalogue entry below). Extended the assertion to also pin
  `categoryId: ""` and `clearReceivedAmount: false`, and added a dedicated
  new test for the case those two fields don't cover between them — a
  transfer edited into a different kind, which must send
  `clearReceivedAmount: true`. All three fixes mutation-confirmed: each
  breaks exactly the test built to catch it and no other.

- Same slice, Task 16 review round: a human reviewer found two more gaps the
  advisor pass above had not, both the same shape — a row in
  `FEATURE_TRACKER.md` said ✅ ("built and verified") for a path nothing
  verified. First, **`POST /api/v1/transactions` had no registered route in
  the test file at all, and no test clicked "Add transaction"** — `handleCreate`
  existed and was wired correctly, but was provably untouched by the suite;
  "Add transaction (modal)" was marked ✅ regardless. Second, and worse
  because it was a real bug, not just an absent test: rows loaded via "Load
  older transactions" are held in local state (`olderRows`) outside the query
  cache the update/delete mutations invalidate — so a transaction edited or
  deleted while it happened to be showing on an appended page kept displaying
  its stale, pre-edit value (or kept existing at all, for a delete) with no
  staleness indicator, until a full reload. This was disclosed honestly in
  the task's own report, but landed in the wrong place: `CLAUDE.md`'s own
  rule is that a feature shipped with a known gap is 🟡 **with the gap
  named**, not ✅ with the gap described only in a report nobody reads
  before trusting the tracker. Both fixed rather than left as a documented
  gap: `handleUpdate`/`handleDelete` now patch or remove the matching row in
  `olderRows` directly from the mutation's own response, and a dedicated test
  for each (create; edit-on-an-older-page; delete-on-an-older-page) was added
  and red-before-green'd by reverting each fix in turn and confirming the
  exact test built for it — and only that one — went red. The general
  lesson: an honestly-disclosed known gap is not the same thing as a correctly
  marked one, and "the report names it" does not substitute for the row
  itself saying 🟡 and why.
- Same review round, a design decision rather than a defect: `TransactionFilters.tsx`'s
  Kind control was first built as a native `<select>`, on the reasoning that
  a single settable value was the only way to keep it both keyboard-reachable
  and queryable by label — a real constraint, but one that traded away the
  design's own segmented control without asking first. Escalated rather than
  assumed; the product owner ruled for rebuilding the real control (a
  `<fieldset>`/`<legend>` grouping three real `<input type="radio">`s, one
  per option, each independently queryable via
  `getByRole("radio", { name: "Income" })`) — proof that the "native `<select>`
  for testability" pattern this codebase leans on elsewhere (`AccountModal`'s
  Owner/Type) is a default, not a rule that overrides the design outright
  when a fully keyboard-reachable alternative exists.

- Task 17's own plan quoted a router test — a caller lacking the `money`
  capability visiting `/money/transactions` must be refused — as the proof
  that the new route sits under `moneyGuardRoute` rather than hung off the
  shell beside it. Run before Step 3 added the route at all, it passed
  outright: `/money/transactions` already fell through to `moneySplatRoute`,
  the Money placeholder's own catch-all, which is *also* a child of
  `moneyGuardRoute` and is gated by the identical `RequireCapability`
  component. Rejection for a capability-less caller was never in question;
  it was true before this task touched anything, for a reason unrelated to
  the row it was meant to pin. The test still does the job the brief
  actually wanted — mutating the route to hang off `shellRoute` directly
  (confirmed by editing `router.tsx` and rerunning) leaves it red, because
  an ungated `TransactionsPage` then tries to mount and hits stub routes
  that were never registered — but by itself it gave no red/green signal
  for "does the dedicated route exist," only for "is whatever handles this
  path gated." Fixed by adding a second, positive test alongside it: a
  caller who *does* hold `money` must actually see `TransactionsPage`'s own
  content at that path, not the Money placeholder's. That one genuinely
  failed before Step 3 and passed after — the pair together is what proves
  both "the route exists" and "it's gated," where either test alone proves
  only one.

**One feature — Transactions, Tasks 5 through 17 — accounts for nine of the
entries above this line, more than any other piece of work in this log.**
Counted, not estimated: nine dash-bullets above are Transactions' own — the
two Task 5 entries, Task 7, Task 8, Task 11, Task 13, Task 15, one from
Task 16, and Task 17. A tenth Transactions bullet sits among them (the Task 16 review
round entry naming a `POST /api/v1/transactions` route with no test behind
it at all, and a stale-display bug on an appended page) and is deliberately
not one of the nine: it is a route nobody wrote a test for, and a shipped
bug, not a test that ran and passed without discriminating — a different
failure than the one this section is about, not a repetition of it. An
eleventh, the segmented-control rebuild, is a design decision rather than a
defect and was never a candidate.

Walked in order, the nine are not nine copies of the same mistake:

- **A serialised race** (Task 5's first entry) — concurrent goroutines that
  all paid the same one-time connection-dial cost before any of them raced
  anything.
- **A short-circuit** (Task 5's second entry) — a count check reached, and
  returned, before the clause under test.
- **A confound** (Task 7) — a household-level delete proved the wrong
  foreign key, because a second, indirect cascade reached the same row
  first and left nothing for the direct one to restrict against.
- **A fixture with no case to discriminate** (Task 8) — ten transactions on
  ten distinct dates left a date-only cursor and a row-value cursor nothing
  to disagree over.
- **A value never set** (Task 11) — a fake double never told to simulate the
  condition its own test claims to exercise.
- **An unwired fixture** (Task 13) — a test harness's own dependencies left
  nil, caught only because a loose assertion happened not to distinguish
  `500` from success.
- **A value never set, again** (Task 15) — a `<select>` option chosen that
  did not exist in the fixture list, a silent no-op in both jsdom and a real
  browser.
- **Task 16's one bullet bundles two, not one** — counted once here, by
  bullet, since the count above is of bullets rather than of individual
  defects: **an assertion satisfied before the failure it should have
  caught** (a spy recording the request before the response it stubbed
  threw), and, separately, **an assertion too loose to pin the one claim it
  named** (`objectContaining` true of a request that also carried a wrong
  `categoryId` and a hardcoded `clearReceivedAmount`, missing the exact
  pointer-semantics translation the task existed to get right).
- **A shared guard** (Task 17) — a route refused for a reason that had
  nothing to do with the row the test was named after.
- Budget slice, Task 2 (the `budgets`/`budget_lines` migration): review found
  both `UNIQUE (household_id, month)` and `UNIQUE (budget_id, category_id)`
  tested with only one case each — insert the pair, insert it again, assert
  the second insert fails. A **single-column** `UNIQUE (household_id)` or
  `UNIQUE (budget_id)` would have failed that exact test identically, since
  the test's own two rows never varied the first column while holding the
  second one fixed; the test proves "this pair collides," not "both columns
  are in the key." Fixed by adding a same-`household_id`/different-`month`
  case (and the `budget_lines` sibling), which only a real two-column key
  passes. The same review also found `expected_income_minor`'s nullable
  check asserting `is_nullable = 'YES'` from `information_schema` without
  ever inserting a real `NULL` and reading it back — true of a column that
  merely permits NULL and one that actually stores it identically — and the
  schema tests' `newBudget`/`newCategory` helpers closing over the parent
  `*testing.T` rather than each subtest's own, so a `Fatalf` inside one
  subtest's setup would have failed whichever subtest happened to run last,
  not the one that actually broke.

None of the nine were caught by the test itself failing; every one needed a
person to ask whether the test could ever have gone red in the first place.

- **The interim Overview (M2), and the plan's own mutation was a no-op.** The
  plan named the mutation for its limited-member budget-card test: remove the
  `isOwner &&` guard on the render and watch it fail. It did not fail. Two
  guards defend that behaviour — `enabled: isOwner` on the `useBudget` call
  and `isOwner &&` on the render — so deleting either one alone changes
  nothing observable, and no single-guard mutation can ever kill the test. The
  deeper cause is what the test asserted: **absence**. `queryByText("This
  month")` is null when the guard works, when the other guard works, and when
  the query simply has not resolved — three reasons, one assertion, no way to
  tell them apart. Retargeted at what actually does the work, through the
  fetch stub's own `capture` hook: the budget endpoint must never be
  *requested* for a limited member. That version dies the moment `enabled`
  stops gating the query. **An absence assertion is satisfied by every reason
  the thing could be missing, including "the page is broken."** When a test
  asserts something is not there, ask what else would produce the same green.
- **Same milestone, the same weakness with a visible cost.** Three tests
  covered a limited member holding `money`: no budget card, no checklist, no
  "+ Add". All three passed. All three would have passed against the page that
  actually shipped to the browser, which rendered the word "Overview" and
  nothing else — no summary (the server omits it for that caller), no budget
  card, no checklist, so every absence held perfectly over a blank page. A
  browser walk found it in one look. The fix was a test that asserts
  **presence**, and a panel that says what is true. Three absence tests over
  one member state is a blind spot, not coverage.
- **And the fix's own sibling, one JSX block away, found by review rather than
  by either.** Gating the new panel on `accounts.isSuccess` was deliberate —
  `summary` is equally undefined while the request is in flight, so the looser
  condition would flash it at an owner. The setup checklist directly beneath
  it had no such gate: `hasAccount` and `hasBudget` are both `false` while
  their queries are in flight, which is indistinguishable from "this household
  has neither", so an **established** household was told to create the account
  and budget it already owned, on every cold load, until the figures landed.
  Same root cause, same file, same session, and the fix for the first did not
  prompt a sweep for the second — the exact failure this document's pattern 1
  is about. Two further things worth keeping: the flash is invisible to a walk
  that navigates and waits (every step of ours waited ~1.5s, well past
  resolution — it took injecting a delay into `fetch` to see it), and the
  obvious test for it is a trap. Asserting on the *first* render passes
  vacuously, because `me` has not resolved then either and nothing owner-only
  is on screen at all; the window that matters is the render after `me` lands
  and before the money queries do, and a test has to hold that window open
  deliberately. **"Loading" is not one state.** A page with three independent
  queries has a lattice of them, and the harmful one is rarely the first.

- **Goals, four separate tasks, one shape each time: an assertion that would
  have passed against broken code.** None of the four was caught by the test
  itself going red — every one needed a person, usually a reviewer, to ask
  whether the test could ever have failed in the first place.
  - Task 2's plan-mandated case for `GoalProgressPercent` was named "net
    negative floors at 0, never a reversed ring" (`goal_test.go:72`) but
    supplied `contributed = -500000` against `target = 400000` — division
    produces a negative percentage on its own before any flooring runs, so
    the expected value of `0` matched whether or not the floor existed. The
    owner's ruling: fix the test, not the plan — a case named for a guard
    that passes with the guard deleted protects nothing. Fixed by adding a
    case where the unfloored arithmetic would land somewhere other than
    zero, confirmed red with the floor removed and green with it restored.
  - Task 6's sharpest finding: a test asserted
    `GoalView.Goal.PlannedMonthly.Currency` to pin "the card stays in its own
    currency, never the household's primary." The field is real and
    populated, but `GoalService.List` never *writes* it — `GoalView.Goal` is
    the stored record's own `domain.Goal` carried straight through
    unconverted, so the currency the test read back was whatever its own
    fixture had put there, regardless of what `List`'s conversion logic did
    or didn't do. The assertion could not fail no matter how `List` was
    written, because that field sits entirely outside the path the service
    under test actually computes. Fixed by asserting the fields `List`
    genuinely constructs itself from `g.Target.Currency`
    (`Contributed.Currency`, `RequiredMonthly.Currency`, including the real
    `ok=false` branch), each mutation-checked by swapping in the household's
    primary currency and watching the corrected assertions go red.
  - Task 7's implementer caught its own instance before review did:
    contributions were asserted on a goal id the test had never seeded, so
    the list was structurally empty regardless of what
    `GoalService.Contributions` did with it — the same "value never set"
    shape pattern 1's Transactions catalogue already carries twice. Fixed in
    the same commit (`f8c1cd6`) by seeding the goal the assertions actually
    named.
  - Task 9's two rollover 404 tests asserted only `status == 404` and the
    error `code`, never the message — indistinguishable between
    `BudgetService.RollOver` refusing with its own `"That could not be
    found."` and chi's route catch-all answering `"That endpoint does not
    exist."` for a route that was never registered at all. Deleting the
    route registration left both tests green. Fixed by asserting the
    service's own message text, with a route-deletion mutation as the proof
    it now discriminates (commits `f1dbd00..8839c46`).

  All four are filed here rather than as new patterns, because the shape is
  the one this section already tracks — an assertion satisfied by more than
  the one thing it claims to pin — not four new discoveries.
- **The same NOT_FOUND-message shape recurred in a different feature — Bills,
  Task 10 — which is why it gets its own bullet here rather than a fifth
  sub-item folded into "Goals" above.** `router.go`'s catch-all "That
  endpoint does not exist." and `errors.go`'s `domain.ErrNotFound`
  translation "That could not be found." share the same `NOT_FOUND` code by
  design — both really do mean "there is nothing here" — which means a
  never-wired `/bills` route 404s exactly like a real service refusal, and a
  test asserting only `{404, "NOT_FOUND"}` cannot tell them apart. Found
  while confirming every one of Bills' seven new routes was genuinely wired,
  not merely answering the right shape some other way. The fix is the same
  one line each time: assert the message, not just the code, whenever a
  test's whole job is proving a route exists.
- `stubFetchRoutes` "throws on an unregistered request, so a query that
  fires when it should not fails loudly" is true only for a component that
  *renders the error*. It is false for one that renders nothing on missing
  data, which every member-state gate in this codebase does on purpose
  (Overview's cards all return `null`/omit themselves while their query is
  disabled or still loading — the exact shape pattern 15 and the interim
  Overview defect both required). Task 16's `NextBillCard` mounts
  unconditionally and calls `useBills(false, { enabled })` itself; with no
  `GET /api/v1/bills` route registered, a wrongly-`enabled: true` query
  still fires, the mock still throws, but that throw becomes a rejected
  promise TanStack Query absorbs as error state — `!bills.data` is true
  either way, so the card renders null regardless of whether the request
  was ever sent. Twelve of fourteen `OverviewPage` owner-role tests passed
  with `GET /bills` silently erroring on every one of them, discovered only
  because two *unrelated* assertions (the quick-add menu's new Bill button)
  happened to fail for their own reasons and forced a full run. The
  "register no route" instruction in the task brief is necessary but not
  sufficient: what actually discriminates enabled from disabled is reading
  `fetchMock.mock.calls` directly (`.some(([url]) => ...)`) — `vi.fn` records
  a call before its implementation throws, so this catches the request
  regardless of what the component does with the failure. Confirmed by
  mutation: forcing `enabled: true` left `toBeEmptyDOMElement()` passing and
  only the `fetchMock.mock.calls` assertion went red.

- Mobile-responsive round: **a gate that runs before the change it exists to
  catch cannot fail on it, however good the gate is.** Every slice of that
  round was supposed to end with a 1440px desktop-parity screenshot compared
  against a pre-change baseline, precisely so an unlicensed desktop movement
  could not ship. One did anyway — `PageContainer` dropped eight pages from
  32px to 24px of vertical padding — and it survived to the *final*
  whole-branch review. The reason is visible only in file timestamps: the
  per-page parity diffs were written at 19:13, and the commit that moved the
  padding landed at 19:58. The gate had run, passed, and been reported as
  passing, forty-five minutes before the change existed. Nobody skipped a
  step and nobody lied; the artefact simply described an earlier tree than
  the one being claimed for. **A verification artefact is evidence about the
  tree it was produced from, and nothing else** — so a gate is only a gate if
  something ties it to the commit it certifies. Timestamps, a recorded SHA,
  or regenerating it as the last act before the commit; assertion alone is
  not enough, and "the parity check passed" was true and useless at once.

- Retros' history-row stand-in (Task 10, `RetrosPage.tsx`) rendered its action
  clause unconditionally: `` `${summary.actionCount} action${...}` `` with no
  guard, so a finished retro nobody added an action to would have shown "0
  actions" — the exact defect family the design's own row spec forbids ("never
  renders `0 actions`"), sitting right next to a mood clause and a quote clause
  that *were* correctly guarded a few lines above it. Every test that touched
  this row used `actionCount: 3`; nothing ever supplied 0, so the gap shipped
  and stayed green through a full lint-and-test pass. Caught while building
  Task 11's `RetroHistoryList` and porting the row markup out of the stand-in,
  not by a failing test — the same way pattern 1's sibling defects are usually
  found, by rereading code a later task has to touch anyway rather than by the
  original suite noticing. Fixed by guarding on `actionCount > 0`, matching
  the quote clause's own `summary.quote ? ... : null` shape, with a test
  (`RetroHistoryList.test.tsx`) that supplies `actionCount: 0` specifically to
  make sure the gap cannot reopen unnoticed.

- **A guard that correctly refused the empty state, in front of nothing.**
  `GoalsCard` computes `hasAnyGoals` from `goals.goals.length` rather than from
  the summary counts, and its own comment explains exactly why: the backend
  counts an achieved goal in neither `datedCount` nor `noDateCount`, so a
  household whose only goal is funded has real goals and all-zero counts. The
  guard was right. Behind it were three clauses — the on-track figure, the next
  dated goal, the no-date count — and an achieved goal satisfies none of them,
  so the card rendered its heading over blank space. The component reasoned its
  way to the exact state twice, in two separate comments, and then had no
  branch for it; a test even pinned the state, asserting only that the *empty*
  state did not appear, with a comment calling the heading-alone render "the
  honest render". **Proving a component does not take the wrong branch is not
  proving it takes a right one** — assert what it says, not only what it does
  not say. That makes at least the fifth zero-render defect in this product.

- **The silent-absorption class, three instances in one feature.**
  `stubFetchRoutes` throws on an unregistered route *before* any capture
  runs, and across three separate Retros tasks that throw was then swallowed
  rather than surfaced, leaving the suite green each time. First (Task 13):
  `TestSaveDraftLeavesItADraft`'s whole claim — that Save never also
  finishes the retro — could not fail, because the `complete` route was
  never registered: a `handleSaveDraft` that also called `finishRetro` would
  throw on the unregistered POST, and `RetroModal`'s own `.catch` swallowed
  it before the test's capture spy ever saw a call. Second (Task 14): a
  previous-month budget route with no registered stub was found only by
  tracing `useBudget`'s *second* query — the one `MoneyCheckInPanel` also
  fires — not the obvious first one. Third, same task: `useHouseholdMembers`
  fires unconditionally the moment `RetroModal` mounts, was unregistered in
  every one of that component's tests, and TanStack Query turned the thrown
  fetch into `members.error` with zero visible failure — the same shape as
  the first instance, a query boundary absorbing the throw instead of a
  component's own `catch`. **The fix is the same in all three: when a
  test's whole claim is that a call was *not* made, register the route
  anyway.** An unregistered route makes a wrongful call indistinguishable
  from a suite that never wired the fixture in the first place; a registered
  one lets the wrongful call actually get recorded, which is the only way
  the assertion means what it says.
- **A mutation the plan specified could not go red, and the reason was a
  second mechanism nobody had named as covering for it.** The net worth
  trend round's plan called for mutating away the `i != trendMonths-1` guard
  in `networth_trend.go` — the guard that stops the newest month from being
  reconverted — expecting the newest bar to then disagree with the headline
  figure. It stayed green. `AccountService.Summary`'s own loop already
  converts every counted account into the primary currency *before*
  `trend()` ever runs, and the per-request FX rate cache
  (`TestSummaryLooksUpEachRateOnce`, a different task's own guarantee) means
  a second conversion of the same currency inside the same request returns
  the identical cached rate — so removing the guard produced a
  bit-identical result regardless. The guard is still real and still the
  documented mechanism (a live provider is free to answer the same currency
  differently twice in one request, and nothing else stops that), but no
  black-box test in this package can discriminate its removal while the
  cache exists, and building a white-box test that disables the cache to
  force it red would only ever be re-testing the cache. Recorded rather than
  chased into a test that cannot exist: the code comment for the guard now
  says so explicitly, and the mutation-that-cannot-fail was reported as a
  concern instead of as a pass. **A mutation that cannot go red is not
  evidence the code is correct — it is evidence the test is not the thing
  discriminating it,** and the fix is to say which mechanism *is* proven to
  do the discriminating (here, the cache's own test), not to keep inventing
  mutations against the wrong guard.
- **A test exercised the defensive branch and still could not have caught a
  realistic mistake inside it.** The same round's archived-account movement
  test drives `deltasByAccountMonth`'s "this account isn't in the counted
  set, skip its movement" branch — the one that keeps an archived account's
  transactions out of every bar — and passes. But the test only asserts that
  the excluded account's movements are absent from the result map; it never
  asserts anything about the *other* accounts' movements being unaffected.
  A currency-preserving mistake inside that branch (for instance, summing
  the excluded account's delta into a neighbouring account's bucket instead
  of discarding it) would still leave the test green, because the real
  guarantee — "excluded accounts are in no bar" — actually lives one level
  up, in `trend()`'s own `for _, a := range counted` loop, which never looks
  at an account outside that set in the first place. The branch inside
  `deltasByAccountMonth` is correct today, but the test covering it proves
  only that the branch *runs*, not that it does the one thing a reader would
  assume from its name. **Executing a branch and discriminating what it does
  are different claims** — a coverage tool cannot tell them apart, and
  neither can a reviewer skimming for a green checkmark next to the line.

- **Every test checked one half of a response against a fixture, so no test
  could fail on the two halves disagreeing.** `GET /transactions` answers the
  ledger and the figures above it in one body, and `handleListTransactions`'s
  own doc comment states why: they "are one screen and must describe the same
  month." `parseTransactionFilter` set the summary's month unconditionally and
  set `filter.Month` only inside `if raw != ""` — and `TransactionFilter`
  documents a zero `Month` as *every* month. So the default request summarised
  August and listed all time, and the screen read "0 in August 2026" above ten
  July rows. The suite was green throughout, because each test asserted the
  list against a list fixture and the summary against a summary fixture, and
  every one of those assertions was true. **A response whose parts must agree
  needs a test that compares them to each other**, not one that compares each
  to something the test itself supplied: the assertion that caught this is
  `summary.count == len(transactions)` plus every listed date starting with
  `summary.month`, and it needed no fixture at all. Note also which check
  would *not* have worked: a count-only assertion stays green whenever both
  halves are wrong in the same direction.

- **A poller test delivered one update and asserted `>= 1`, so it stayed green
  against a poller that had stopped polling.** `TestPollerSurvivesAPanickingHandler`
  fed one update, watched the handler panic, and checked the handler had been
  called at least once. Moving `recover` out of the per-update dispatch so it
  wrapped `Run` instead leaves the poller **dead after the first panic** — and
  the test passes, because one call is at least one. The claim the name makes
  is "the loop survives"; the only assertion that can carry it is a **second**
  update, delivered after the panicking one, arriving at the handler. **A test
  about surviving something must exercise the thing that comes after it.**
- **A backoff test asserted two calls happened, so deleting the entire backoff
  made it pass *faster*.** `TestPollerBacksOffAndKeepsGoingAfterAnError` proved
  the loop retried, which was never in doubt; the behaviour under test was
  *waiting* before retrying, and elapsed time was the one thing it never
  looked at. **When the behaviour is a delay, the assertion has to be about
  time** — a fake clock, or a measured floor on the gap between the two calls.
  A test that a mutation makes *quicker* is measuring liveness, not the rule.
- **A rate-limit test seeded three prior redemptions where two was the boundary,
  so `>` could become `>=` and silently halve the limit with the suite green.**
  The limit refuses at four redemptions in an hour; a fixture starting at three
  is over the line under both the correct comparison and the broken one.
  **A boundary test has to sit exactly on the boundary** — one row below and
  one row above — because a fixture comfortably past it tests the direction of
  the comparison and nothing about where it falls.
- **A refusal test used `strings.Contains(got, "expired")`, so appending text to
  the message shipped an enumeration oracle green.** Telegram's bot answers an
  unknown, expired, consumed, rate-limited or ceiling-blocked `/start` with one
  identical sentence, precisely so none of the five can be told apart by
  probing. Adding "Too many attempts." to the rate-limited branch still
  contains "expired" — the suite passes, and the product now tells an attacker
  which branch they hit. **When the rule is "these answers are identical",
  assert equality between the two answers, not a substring of each.** The
  substring form tests that a message is *about* the right thing; the rule is
  that it is the *same* thing.
- **A double that returned `""` for "nothing was sent" let an identical-answer
  test pass against a service that answered dead nonces with silence.** The
  test compared the reply for a rate-limited chat with the reply for an expired
  one; both came back `""`, which is equal, so the assertion held even though
  one of the two branches sent no message at all. **A "no value" sentinel that
  is also a legal value cannot represent absence** — the double needs a
  recorded slice, or a `(string, bool)`, so "sent nothing" and "sent the
  refusal" are different observations rather than the same empty string.
- **A mutation check proved nothing because an earlier assertion in the same
  test masked it.** Task 4's first-draft rollback test asserted the constraint
  name *before* asserting the household row had survived the failed
  transaction. Under mutation the constraint-name check fired first, the test
  went red, and the red looked like proof — but it was proof of the incidental
  assertion, not of the rollback. **Order assertions so the one under test
  fails first, or split it into its own test.** A mutation is only evidence
  when the failure it produces names the claim you meant to make.
- **A plan-mandated clamp was dead code, so the test written for it could not
  fail — admin households branch, 2026-09-02.** `relativeTimeLabel`'s plan
  called for `Math.max(0, now - iso)` to stop a clock-skewed future timestamp
  rendering as a negative age, and for a test asserting the clamped result.
  Both shipped in the same task. But the function's *first* branch is
  `if (elapsed < MINUTE) return "just now"`, which any negative number
  satisfies — so the clamp changed nothing any caller could observe, and no
  offset and no assertion could tell clamped from unclamped behaviour. The
  test was green against code with the clamp and green against code without
  it. The fix was to delete the clamp, state the property in the comment
  ("a future timestamp reads 'just now', because every negative elapsed value
  falls into the first branch — keep that branch first"), and rewrite the test
  as a five-day-future timestamp, which a plausible refactor
  (`Math.abs(elapsed) < MINUTE`, "for symmetry") turns into `-7200 min ago`.
  That mutation was run and the new test went red on it. **A guard placed
  behind an earlier branch that already handles the case is not a guard, and
  a test for it is a test of nothing** — the property is worth pinning, but it
  has to be pinned to the branch that actually does the work.
- **A mutation went red for the wrong reason and nearly passed as proof —
  same branch, Task 1.** `TestTouchWritesOnlyLastSeenAt` exists to prove
  `TouchSession` writes one column. The planned mutation set
  `expires_at = $2`, the same touch-time value — which made the session
  expire immediately, so `GetLiveSession`'s own `expires_at > now()` filter
  dropped the row and the test failed three lines earlier, at the fetch, with
  "ByTokenHash after touch: not found". Red, but red for a reason that never
  reached the `ExpiresAt` assertion under test. Re-running with
  `expires_at = now() + interval '90 days'` — tampering with the column while
  keeping the row fetchable — produced the intended failure, and a third
  mutation on `admin_grant_expires_at` pinned the last assertion. **When the
  thing you tamper with is also what makes the row readable, the mutation has
  to keep the row readable** or the test fails before it can testify.

- **A mutation-check recipe told an implementer to remove behaviour without
  saying what to do with the import it orphaned — outbound message
  inspector, Task 3, 2026-09-04.** The plan's own mutation step for
  `MailpitOutbox.Message` read "drop the escaping — change
  `url.PathEscape(id)` to `id`", to prove
  `TestMailpitOutboxEscapesTheMessageID` actually pins escaping. Applied
  literally, `net/url` becomes an unused import and the package fails to
  *build*: `go test` printed `FAIL`, but for
  `"net/url" imported and not used`, with zero test functions having run —
  not even the six unrelated ones in the same file. This is the same shape
  as the `-run TestHousehold` filter above (a command that prints `FAIL` or
  "no tests to run" is not evidence a test executed), sharper here because a
  *build* failure is even easier to mistake for the test doing its job: the
  implementer asked for red, saw red, and the wrong kind of red still reads
  as success unless someone checks which line produced it. Caught by
  reading the failure's own text rather than trusting the exit code — it
  named an import, not the assertion the mutation was supposed to break.
  Fixed by keeping `net/url` referenced (`_ = url.PathEscape`, commented as
  existing only to hold the import for this check) while changing the real
  call to `"/api/v1/message/" + id`, which reproduced the intended, isolated
  failure at the escaping test's own assertion, with the other six tests
  still green. Recorded as a finding rather than smoothed over, and Task 4
  and Task 5's own mutation checks each recorded a line noting no
  import-orphan risk applied to their own mutations, rather than repeating
  the mistake unchecked. **A mutation-check recipe has to say what happens
  to the import it orphans, not only what to change** — a build failure and
  a targeted test failure can print the same word, and only one of them
  proves the test does its job.

- **A mutation check silently reused Go's test cache and reported a false
  green — `hearth_readonly` role, Task 1 of the database browse,
  2026-09-04.** The mutation step was "delete the `GRANT SELECT ON ALL
  TABLES` line from `deploy/readonly-role.sql`, re-run, expect the `reads`
  subtest to fail." The re-run command was the same `go test -run
  TestReadOnlyRole -v` used for the original green — no `-count=1` — and it
  printed `PASS` with `ok ... (cached)`. That was not evidence the mutation
  did nothing: `deploy/readonly-role.sql` is read from disk with
  `os.ReadFile` at test *runtime*, not compiled in, so it is invisible to
  the Go toolchain's build-graph cache key — editing it looks, to `go test`,
  like editing nothing. Caught only because the cached-result line
  (`(cached)`) was read rather than just the `PASS`/`FAIL` word — the same
  discipline pattern 2's `-run TestHousehold` and import-orphan entries
  already name, one layer further down the stack. Re-run with `-count=1`
  and the same mutation correctly failed the `reads` subtest with "households
  not visible to hearth_readonly; the GRANT did not run." **A mutation check
  against a file the test reads at runtime rather than imports needs
  `-count=1` on the re-run, every time** — the build graph has no way to know
  the file changed, so the cache will hand back the old answer and call it
  passing.

- **An assertion against a list of absolute paths, written with relative
  paths — database browse, Task 9, 2026-09-04.** `adminBundleSplit.test.ts`
  walks `main.tsx`'s import graph and asserts the admin chunk is not
  reachable from it; the whole point is that no household member downloads
  the operator surface. The task's brief added two new guards using bare
  strings — `"features/admin/AdminDatabasePage.tsx"` — against a `reachable`
  set whose every entry is an absolute path built with
  `join(SRC_ROOT, …)`, which is what all eight pre-existing assertions in
  that file already do. A relative string can never equal an absolute one,
  so the new guards would have passed **forever, whatever `main.tsx`
  imported**: the day someone statically imported the admin page into the
  entry bundle, the test protecting against exactly that would have stayed
  green. Caught by the implementer noticing the shape mismatch against the
  file's own convention, then proved by making the assertion fire. What
  would have caught it sooner: **when adding an assertion to an existing
  test file, construct the expected value the way that file already
  constructs it** — the eight neighbours were the specification, and the new
  lines were the only two that did not follow it.

- **A paging test that could not be made to fail by adding rows — database
  browse, Task 5, 2026-09-04.** `Rows` orders by the primary key (`ctid`
  when a table has none) because `LIMIT`/`OFFSET` without `ORDER BY` lets
  Postgres return rows in any order it likes, so page 2 can repeat a row
  from page 1 and skip another with nothing raising an error. The mutation
  was "delete the `ORDER BY` clause, watch the two-page test go red." It
  stayed green — and the controller's instruction to fix that by raising the
  row count was **wrong**, which is the part worth writing down. Measured: 5
  rows and 500 rows both pass with `ORDER BY` deleted. A static heap read by
  one connection comes back in the same physical order at any size, and the
  one thing that would shuffle it, a synchronized sequential scan, needs a
  table larger than `shared_buffers / 4` — a size this schema will never
  reach. **The discriminator is not size, it is a write between the two
  pages.** An `UPDATE` writes a new row version and allocates a new line
  pointer *after* the existing ones, so an unordered sequential scan then
  returns `[a3, a4, a5, a1', a2']`, and `offset=2 limit=2` yields
  `a5, a1'` — the exact "a row appeared on two pages" failure, deterministic
  and at five rows. That is also what a live operator screen actually meets:
  the product is writing to these tables while somebody is paging through
  them. What would have caught it sooner: **when a mutation stays green, ask
  what physically produces the behaviour under test, not how to produce more
  of the input** — more rows was more of the same evidence, and the answer
  was a different kind of event entirely.

- **The line position of a guard is not what makes it correct — database
  browse, Task 9, 2026-09-04.** The row viewer must render "no such table"
  for a 404, and `isAdminLayerFailure` counts `NOT_FOUND` among the failures
  `AdminGate` owns, so the filtered error is null for a 404. The plan stated
  the rule as *ordering* — "`isNotFound` must be checked **before**
  `isAdminLayerFailure`" — and wrote the mutation to match: move the
  `isNotFound` block below the const and expect red. It cannot go red, **by
  construction rather than by luck**: `isAdminLayerFailure` only computes a
  `const`, and the `isNotFound` block already early-returns, so moving one
  past the other changes nothing a test can observe. The real invariant is
  about the *operand*, not the line: `isNotFound` is evaluated against the
  raw `query.error` and never against the gate-filtered value. Mutating to
  *that* — swapping the argument — went red on exactly one case. The same
  branch had a second inert assertion beside it: the test asserted no
  password prompt appears, inside a render tree where `AdminGate` is never
  mounted, so no password prompt could ever have appeared. Both were
  replaced. `AdminMailPage.tsx` carried the identical "checked FIRST"
  wording one file away, inviting the next reader into the same
  misunderstanding, and was reworded in the same change (pattern 1: fixing
  an instance is not fixing the class). What would have caught it sooner:
  **write the invariant as a sentence about values, then check the mutation
  actually violates that sentence** — "checked first" describes the source
  file, "reads the raw error, never the filtered one" describes the
  program, and only the second one can be broken on purpose.

**Mutate to prove a test.** Break the code deliberately, watch the test go red,
restore it. If it stays green, the test is decoration — and if it goes red for
a different reason than the one you meant to prove, that is not yet proof
either; sharpen the mutation until the failure names the claim. **And if the
mutation cannot go red because a second guard covers for the first, the test
is pinned to neither** — mutate the guard that does the work, or assert
something only that guard can produce. **And if the code under test reads a
non-Go file at runtime, re-run with `-count=1`** — Go's test cache does not
know that file is an input, and will hand back the previous result.
**And if the mutation is written as "move this line", suspect it before you
run it**: position is a property of the file, and a test can only observe
properties of the program. Three of the mutations in this section's newest
entries were unfalsifiable for that family of reason — a string compared
against a differently-shaped string, an assertion about a component that was
never mounted, and a reordering of two statements that do not interact.

### 3. The simulated environment lied

- `Modal` threw `InvalidStateError` on **every open in every real browser** —
  React renders `<dialog open>` before the effect calls `showModal()`, and the
  spec throws if the attribute is already there. All five tests passed, because
  jsdom's `HTMLDialogElement` is an empty stub with no `showModal` at all. Only
  the fallback path ever ran.
- Fixing that exposed a second bug that had been unreachable: the dialog never
  stretched to the viewport, so there was **no backdrop area to click**.
- The 401 redirect handler bounced every invitee off the invite screen. Green
  suite — because the handler defaults to null and every test installed a stub
  instead of the real wiring.
- The accounts browser walk answered `500 INTERNAL` on every route, and **the
  API logged nothing** — impossible on its face, since the only two code paths
  that produce that response (`logAndWriteInternal`, `recoverer`) both log
  before writing. An hour went into re-reading the code for a bug that was not
  there. The cause was not simulated but *assumed*: this machine runs two
  Docker engines, and a five-hour-old Docker Desktop stack was silently
  holding the host ports colima's stack needed, so the browser and every
  `curl` reached stale code while every `docker compose` command managed a
  container nobody could see or log. The tell was in the response the whole
  time — the request ID's hostname prefix never matched the running
  container, and the per-process request counter never reset across a
  restart, neither of which is possible for a process actually being
  restarted — and went unread because nobody checked which process was
  actually answering.
- The Budget walk hit the same trap inverted, with a new twist: colima
  auto-started mid-session and silently took over the **default docker
  context and both socket paths the CLI resolves**, while Docker Desktop's
  hours-old stack kept the host ports. Two successive database wipes wiped a
  fresh colima stack the browser never talked to, and the browser's session
  "survived" the wipe — which read as an impossible auth bug (sessions live
  in Postgres) until `docker info` on both sockets returned the *same*
  engine ID and `docker --context desktop-linux ps` found the real stack.
  The check that settles it in one command: `docker info --format
  '{{.ID}}'` per socket/context, before believing anything else. When the
  engines disagree with the ports, run every stack command with an explicit
  `--context`.

- The Transactions ledger's Kind filter (All / Expense / Income) is a real
  `<fieldset>` of `<input type="radio">`s, built keyboard-reachable on
  purpose — but each radio is `sr-only` (visually hidden), so the `<label>`
  pill wrapping it is the only thing a sighted user sees, and that label's
  className never reacted to the radio's own `:focus-visible` state. Tabbing
  or arrow-keying through the group moved real focus with **zero visible
  sign of it** — `element.matches(':focus-visible')` was `true` throughout,
  while the label's computed `outline` and `box-shadow` stayed `none`.
  `fireEvent.click` in every existing test fires a click directly at the
  element, the same shortcut jsdom's `<dialog>` stub takes, so nothing here
  ever pressed Tab or an arrow key for real. The first fix (a single
  `has-[:focus-visible]:ring-accent`) was itself caught half-wrong by the
  same walk: two screenshots of the selected-and-focused pill, taken before
  and after that fix, came back **pixel-identical**, because a dark-green
  ring inset against the pill's own near-black selected background has no
  contrast. The ring colour had to become conditional on the pill's own
  background (white ring on the dark selected pill, accent ring on the light
  unselected one) before it was visible in both states — a reminder that a
  fix a screenshot diff would have caught in twenty seconds still went out
  the first time, because nobody diffed the screenshots.
- The Budget screen's category and by-person progress bars used
  `bg-hairline` (a 0.08-alpha border tint meant for 1px lines) as a bar
  *track*, and the Categories card heading used `mb-4.5`, a spacing step
  outside Tailwind's default scale. Both looked fine to `npx vitest run`:
  jsdom renders no CSS at all, so nothing catches a class that resolves to a
  colour too faint to see, or to a class that does not exist and generates
  no rule whatsoever. Caught only by loading the real page and reading
  `getComputedStyle` on the rendered elements — `bg-hairline` became the
  app's own opaque `bg-canvas` (matched against a live `rgb(240, 238, 233)`
  read-back) and `mb-4.5` became the explicit `mb-[18px]` the design's own
  spacing called for.
- Finance-fixes round, the grouped sidebar's Money links (pattern 13): the
  brief's own suggested mechanism, TanStack Router's `Link` `activeProps`,
  merges its `className` onto the base `className` rather than replacing
  it, so an active link carried both the base's baked-in `text-ink` and
  `activeProps`' `text-accent` at once. Tailwind's cascade resolves color by
  which utility's generated rule sits later in the *stylesheet*, not which
  class token is later in the *class list* — `text-ink` always won,
  regardless of which link was active. Every Sidebar test stayed green,
  because a `toHaveClass("text-accent")` assertion only checks that the
  token is present in the string jsdom hands back, never which color a real
  cascade actually resolves to — the exact gap this pattern's own closing
  sentence, written after the Kind filter's ring-colour near-miss, already
  named. Found only by opening a real browser and reading
  `getComputedStyle` directly: on `/money`, Finances computed to
  `rgb(26, 107, 82)` (accent) and Transactions to `rgb(28, 27, 24)` (ink);
  on `/money/transactions` the two should have reversed and did not —
  `text-ink` was showing on whichever link was actually active, either way.
  Fixed by making `NAV_ITEM_CLASS` layout-only, with every caller stating
  its own single color class, and computing each Money link's active state
  itself via `useMatchRoute` instead of `activeProps` — one color class per
  link leaves nothing for the cascade to arbitrate.

- UX-repair round (M1): **the app had no maximum content width at all.**
  `AppShell`'s grid gave the sidebar 236px and the page `1fr`, so every page
  stretched to whatever monitor it was opened on. Measured in a real browser
  at 2752 CSS px, the Transactions ledger's "All transactions" heading sat at
  x=170 and its "+ Add transaction" button at x=2577 — **2407px apart**, a
  heading and the button that acts on it at opposite ends of the desk — and
  the Settings notification toggles sat at x=2653, far from the labels naming
  them. The entire frontend suite was green throughout and could not have been
  anything else: jsdom performs no layout, so `getBoundingClientRect()` returns
  zeroes and there is no width for an assertion to be wrong about. Fixed by
  bounding the outlet to the design's own 1204px column (a 1440px canvas less
  the 236px sidebar), which moved the same two elements to x=928 and x=1921 —
  993px apart. Two things generalise. First, *nothing on the layout axis is
  testable in jsdom*, so "the tests pass" carries no information at all about
  it, and the only witness is a measurement taken in a browser at a realistic
  viewport — including a wide one, since the defect is invisible at 1280px
  where most people develop. Second, a design's own numbers are a
  specification: 1204px was in the mockup from the beginning, and the defect
  is simply that nobody transcribed it.

- Mobile-responsive round, before Task 1: **a fixed-width shell made every
  page look broken, and the whole round exists because of one class.** The
  symptom was "the UI is not mobile friendly"; measured in a real browser at a
  375px viewport (360px client area after the scrollbar), `AppShell`'s
  `grid-cols-[236px_1fr]` gave the sidebar its full 236px unconditionally and
  left `<main>` **124px** wide — and after the page's own `px-9` gutters, 52px
  of usable content, where Overview rendered roughly one word per line and its
  net-worth figure read `S$0.` with the value cut off. With the sidebar hidden
  by hand and `<main>` given the full 375px, overflow on the Transactions page
  dropped to **zero** — the ledger's rows were already `flex`-with-two-children
  and wrapped on their own. The shell was not merely the worst defect on the
  branch; it was most of the defect (`docs/superpowers/specs/2026-08-15-hearth-mobile-responsive-design.md`,
  "What is actually broken"). What would have caught it nine slices sooner:
  opening the app at a phone width once, at any point before this round
  started.

- Mobile-responsive round, Task 8: **the plan's own prescribed fix broke the
  thing it was fixing, and only a real browser caught it.** The brief's
  `TransactionFilters.tsx` fix gave each filter's wrapper `flex-1` (Tailwind's
  `flex: 1 1 0%`) plus `min-w-0`, reasoning that `min-w-0` was "what actually
  permits the shrink." It compiled, typechecked, and passed all 477 tests
  unchanged, because nothing in jsdom lays out a flexbox. In a real browser at
  320px it did the opposite of the intended fix: a `0%` flex-basis makes
  flex-wrap's line-breaking treat the item as contributing no size, so the
  four filters never got their own line — all four crammed onto one shared
  line and shrank to it, and their labels ("ACCOUNT", "CATEGORY") clipped to
  a measured 15px sliver instead of the row wrapping cleanly. Replaced with
  `w-full`/`sm:w-auto` on the wrapper — the same pattern already used on the
  select inside it — since an item demanding the full row's width can only
  ever wrap onto a line by itself. The brief's own worked example also didn't
  hold up: its comment asserts an account nicknamed "Joint everyday account"
  (23 characters, measured 181px) would overflow the row, but it does not, at
  either 375 or 320 — the mechanism is real (a 47-character nickname measured
  326px and did overflow 320's 273px row), the specific number in the comment
  was not. Comments that assert a measurement are exactly the kind of claim
  this pattern exists to make someone re-check in a browser before trusting.

- Mobile-responsive round, Task 8: **`BudgetStatCards.tsx`'s four-card row has
  been logged twice as a pre-existing `md:` breakpoint to leave alone —
  Task 6/7's report, and Task 8's own dispatch note, which told this task's
  implementer to leave the file alone — without anyone measuring whether it
  actually overflows** — because no earlier task had real budget figures to
  render there. It does, and the cause isn't the `md:` prefix: the
  *unprefixed* `grid-cols-2` base is too narrow for what the cards actually
  hold. `formatMoney` output like `S$3,790.00` has no space to wrap on, and
  CSS Grid's implicit per-item `min-width: auto` (the same default that made
  a `<select>` unshrinkable in the bullet above) keeps a 90px cell from
  shrinking below that text's ~125–148px min-content width — so the *grid*
  grows past its container instead. Measured at 320px: 60px of page-level
  horizontal scroll, the largest violation found in this task, worse than
  either defect this task was assigned to fix. The dispatch note's "leave it"
  meant the file's stale `md:` breakpoint, not a floor violation — nobody had
  measured this one when that note was written, and the spec's 320px floor is
  binding, which outranks a dispatch note written before the number was
  known. **Assigned to Task 9**, not left as a standing deferral: whoever
  picks it up should start from "unbounded min-content in a narrow grid
  cell," not from the breakpoint count.

- Mobile-responsive round, Task 10: **jsdom cannot see a breakpoint.** The
  frontend suite held 47 test files and 477 tests, all green, through every
  task of this round — an off-canvas drawer, shrinking auth cards, `dvh`
  sizing, stacking gutters and field pairs, a 44px touch-target floor — on a
  product that could not be used on a phone at all until the round started.
  Every layout defect the round found, including both bullets immediately
  above, was invisible to that suite the entire time it stayed green, because
  none of them is a claim jsdom's stub layout engine can evaluate. A layout
  claim needs a browser to check it; a green frontend suite is not evidence
  about layout, on this branch or any other.

- Mobile-responsive round, Task 4: **`100vh` is a lie on iOS Safari, and
  nothing in this project's own tooling can be asked to lie back.** Every
  measurement behind this round's plan was taken in a real browser — headless
  Chrome, resized narrow — which is correct and was still not enough, because
  that browser has no address bar to hide and so no way to reproduce the one
  thing iOS Safari does differently: `100vh` there is the *large* viewport,
  sized as though the toolbar were already gone, so a full-height box built
  against it puts its bottom edge under the toolbar the moment a real page
  loads with the bar still showing. `Modal.tsx`'s own comment on the fix
  measures the consequence exactly — `AccountModal`'s content at 665px against
  roughly 650px of visible height on an iPhone, its submit button sitting
  precisely where a thumb cannot reach it. This was not found by a failing
  test or a device in hand; it was named in Task 4's own brief before any code
  changed, from reasoning about what the CSS property means on that engine,
  because no tool running anywhere in this stack — headless Chrome, jsdom,
  the browser-automation tools used for every other check in this round — runs
  WebKit's dynamic toolbar at all. Fixed by moving every full-height rule
  (`h-screen`/`min-h-screen` → `h-dvh`/`min-h-dvh`) across `Modal.tsx`, every
  auth screen and the route fallback before the gap could be found any other
  way. Worth keeping separate from the bullet above: that one is a defect this
  round's own tooling *could* have caught and initially did not; this one is a
  defect the tooling in use cannot reach at any thoroughness, which is a
  different problem than testing harder solves.

- Mobile-responsive round, final review: **a Tailwind utility written
  immediately before `${` in a template literal is never extracted, in any
  environment — this is not a jsdom gap, it never reached the generated CSS
  at all.** `PageContainer.tsx`'s className was built as `` `flex flex-col
  gap-5 px-4 py-6 sm:px-9 sm:py-8${className ? ... : ""}` `` — `sm:py-8`, the
  last static token before the interpolation, has no delimiter telling the
  scanner where the candidate ends, so it was never added to the stylesheet;
  `sm:px-9`, one token earlier with whitespace on both sides, was. The review
  measured it in a running browser: a probe element computed `padding-top:
  0px` for `sm:py-8` against `padding-left: 36px` for `sm:px-9`, same
  element, same line. All 8 pages using `PageContainer` rendered 24px of
  desktop vertical padding where the design calls for 32px, and all 477
  frontend tests stayed green throughout, because none of them reads a
  computed style. Fixed by hoisting the class string into a `BASE_CLASS`
  constant so no utility ever sits directly against `${` — the pattern
  `SELECT_CLASS`/`NAV_ITEM_CLASS`/`FIELD_CLASS`/`LABEL_CLASS` already used
  correctly elsewhere in this codebase. Confirming it took one step more than
  usual: editing the source and reloading the already-running dev server left
  the computed padding at 24px until the `web` container was restarted —
  itself further proof the class had never been generated, not merely cached
  stale.
- Mobile-responsive round, final review: **a flex child with no explicit
  `flex-grow` stops at content height, and the empty space left in its
  flex-container parent still intercepts clicks meant for whatever is
  underneath.** `NavDrawer`'s panel is `h-dvh` (812px at a 375px phone) and
  `flex-col`; its only child, `Sidebar.tsx`'s `<nav>`, had no `flex-*` class
  and so sized to its own content — measured 516.75px, leaving 295.25px of
  the panel's own box empty. Neither the panel nor that empty region paints a
  background, so the drawer's `bg-black/40` backdrop showed through
  visually — but `document.elementFromPoint()` in that region returned the
  panel, not the backdrop, because an element's hit box is its own layout box
  regardless of what's visually behind it. A tap on 36% of the open drawer's
  height looked like dismissable backdrop and did nothing. Fixed with
  `flex-1` on the `<nav>`, which also happened to fix a second, unnoticed
  consequence: sign-out's real position (317px above the viewport bottom)
  didn't match decision 7's stated reason for using `dvh` on the drawer at
  all. No test could have caught either: `getBoundingClientRect()` and
  `elementFromPoint()` both need real layout, which jsdom does not perform.

- Mobile-responsive round, Task 10: **the width matrix's own check could not
  see the thing it was checking, for one whole class of screen.** The walk's
  rule was `document.documentElement.scrollWidth <= clientWidth` at six widths
  across every page, and it came back clean — while `BudgetModal`'s category
  rows were overflowing horizontally the entire time. A native `<dialog>`
  paints in the **top layer**, outside normal document flow, so nothing
  happening inside it moves the document's own scroll width: the document
  reports 375/375 while the dialog's own box scrolls. The row's flexible name
  field needed `min-w-0` — the same unshrinkable-flex-item trap already on
  record twice in this file, for `<select>` and for `BudgetStatCards` — but
  the point worth keeping is not the fix, it is that **a check inherits the
  blind spots of whatever it measures**. Any sweep for overflow needs a
  second measurement per modal (`dialog.scrollWidth` vs `dialog.clientWidth`,
  and the panel inside it), because the first one is structurally incapable
  of failing there. Six modals were re-measured that way afterwards; only
  this one was broken, which is exactly why nobody would have gone looking.

- Retros, Task 13: **a UI-layer last-write-wins, after the whole backend was
  built to prevent it.** Decision 6 refuses a stale save at the database with
  a `version` guard, specifically so two partners typing into one draft can
  never silently overwrite each other. The conflict banner's own Reload
  button undid that protection one layer up: clicking it cleared
  `useRetro`'s `conflict` flag and re-fetched the retro, but never re-seeded
  the modal's own local `mood`/`wentWell`/`wasHard`/`notes` state from the
  fresh data — so the fields on screen stayed exactly as the first partner
  had left them, and the next Save sent that stale text back carrying the
  *new* version number, overwriting the partner's just-written paragraph
  with no mismatch left to catch it, because the version had just been
  refreshed to match. No unit test would have found this: every existing
  conflict test asserted the banner appeared and Save/Finish were disabled,
  never that a *subsequent* successful save carried the reloaded data. Found
  in a real browser, opening the same draft in two sessions and typing into
  both. Fixed inside the one-way `hadConflict` latch this task already
  ruled for: Reload no longer re-enables editing at all — the typed text
  stays on screen to be copied by hand, and the person has to close and
  reopen the modal to get a genuinely fresh, correctly-seeded editor. **A
  concurrency guard built at the database only protects the request that
  reaches it — a UI control offering to "recover" from the guard's own
  refusal needs the same scrutiny as the write path itself.**

- Net worth trend round, Task 7 to Task 8: **a flagged-but-unverified visual
  risk is not closed until someone looks, and the browser walk is where such
  flags get retired.** Task 7's own progress notes flagged, unprompted, that
  "the percentage renders inline in the 30px figure, and wrapping at mobile
  width on Overview is unverified" — then deferred it to the browser walk
  rather than guessing at a fix, because no vitest assertion on a 30px
  `<p>` containing an inline `<span>` can evaluate real text wrapping; jsdom
  performs no layout, the same gap every bullet in this pattern already
  rests on. The Task 8 walk opened Overview at 360px (a mainstream Android
  width) and 320px and found the badge's own text — `▼ 6.0% this month` —
  wrapping mid-phrase, stranding "month" (or "this month") alone on its own
  line, still in the danger colour, reading as a broken element rather than
  part of the figure. Finances' own copy of the same badge (`▼ 6.0%`, no
  "this month" suffix) did not wrap at either width — the extra words were
  exactly what did not fit. Left unfixed at first, pending a product
  decision, per the instruction not to patch a defect found this late in a
  browser walk without one; the decision came back the same day (render the
  change as its own line under the figure whenever a changeNote is present,
  matching the design's own Overview tile, rather than inline beside a 30px
  figure) and a fix round closed it, confirmed clean at both widths and
  pinned by a test that asserts the badge's own DOM arrangement rather than
  its words, since the words were identical either way and would not have
  caught a regression back to inline. **The lesson is not "test more" — no
  test in this stack can evaluate real text wrapping — it is that naming a
  risk and deferring it is only half the job.** A risk written down and
  never revisited is indistinguishable, to the next reader, from a risk
  nobody noticed; the round that finally opens the real browser the risk was
  waiting for is what turns the flag into either "confirmed safe" or "found
  and fixed," and until that happens the flag is not evidence of anything.

- **UI-polish round: a census, not a test, is what found the gap, and it had
  been open for the entire life of the project.** Across roughly ninety
  components, the frontend carried zero `focus-visible` rules, zero `active:`
  rules, zero `tabular-nums`, and four `hover:` rules — and had shipped with
  Chrome's own default focus ring (`rgb(0, 95, 204)`, 0.8px) inside a
  cream-and-forest palette it never matched, the whole time. No test could
  have caught any of it, because nothing in this suite asserts on an
  interaction state at all — the same gap the Kind filter's own
  `:focus-visible` defect already exposed once in this pattern, since
  `fireEvent.click` fires a click directly at an element and never sends the
  keystroke a real Tab press would. What actually found it was two things,
  neither one a test: counting how many files used each interaction-state
  utility (the zero and four above), and driving the app with a keyboard
  instead of a mouse. **A periodic count of a utility's usage across the
  codebase is a cheap check no test suite can express, and a browser walk
  driven by Tab and arrow keys finds a different set of defects than one
  driven by clicks** — the second is not a more thorough version of the
  first, it is a different instrument pointed at a different gap.
- **Tailwind v4 tree-shakes a `@theme` custom property it does not see used,
  and a redefinition inside a media query does not count as a use.**
  `--transition-state: 160ms` was declared in `@theme` and referenced only
  inside a `prefers-reduced-motion` block that redefined it to `0ms` — no
  plain rule anywhere read the `@theme` value directly, so Tailwind's scanner
  dropped it from the emitted `:root` entirely. `transition-duration:
  var(--transition-state)` then computed to `0s` everywhere it was used:
  every hover transition this round added would have been instant, and the
  reduced-motion guard existed to disable a transition that the tree-shaking
  bug had already disabled for everyone. `tsc`, `eslint`, `vite build` and
  583 tests all passed against the built CSS, because none of them reads a
  computed style — the same gap every bullet in this pattern already rests
  on. Found by probing `getComputedStyle` in a real browser, the only place
  the variable's absence was visible. Fixed by moving the property out of
  `@theme` into a plain `:root` block: `@theme` is for tokens Tailwind itself
  generates utility classes from, and a non-namespaced custom property that
  nothing but a media query ever references is not that.

- **An injected clock did not reach the guard the test depended on, so the
  test rotted on a schedule.** `TestOwnerSeesTheTwelveMonthTrend` pinned
  `movableClock` to `2026-07-28` to make the trend window assertable. It
  passed for thirty days and then failed every day after, with a 401 on the
  very first authenticated request — nothing to do with the trend. Session
  validity is enforced in SQL, not in Go: `GetLiveSession`'s `WHERE` carries
  `expires_at > now()`, *Postgres's* `now()`, and `session_repo.go` already
  said outright that "there is no separate check here". The injected clock
  moved the app's idea of time and left the database's alone, so once real
  wall time passed the pinned date plus `SessionTTL`, the session the test
  signed in with was already expired before it made a single request. The
  fake was real enough to look like control over time and not real enough to
  be it. What makes this worth writing down twice: the test's own doc comment
  claimed the pinned clock gave "a known, reproducible range rather than
  whatever month the suite happens to run in" — the precise opposite of what
  it had. A comment asserting a property is not the property. Fixed by
  anchoring on `time.Now().UTC()` and deriving every asserted month from that
  anchor, which is what the one other `movableClock` caller,
  `TestSessionCookiesSlideWhenExtended`, already did — it moves *forward* from
  real now and has never rotted. **When a fake stands in for something a real
  dependency also owns, ask which of the two the code under test actually
  consults.**

- **`window.open` after an awaited `fetch` is silently blocked on iOS Safari,
  and no test in this suite could ever have seen it.** The "Continue with
  Telegram" button first opened the popup in the mutation's `onSuccess` — the
  natural place, since that is where the URL arrives. WebKit gates
  `window.open` on the **synchronous user-gesture call stack**, and an awaited
  network round trip reliably breaks that gate; this is the textbook
  OAuth-popup-blocked-on-Safari failure. Chromium's transient-activation
  window is looser, so on a fast desktop it usually works, which is exactly
  what makes it ship. **jsdom's `window.open` is a stub with no user-activation
  model at all** — there is no state for an assertion to read, so a suite of
  green tests is blind to the feature simply not working on the phone it was
  built for. The shape that survives: open a blank tab **synchronously inside
  the click handler**, before the fetch, then point `popup.location.href` at
  the URL once the response lands. And because a hard-blocked popup still
  returns `null`, the null branch has to do something visible — here, render
  the URL as a real link — or the failure this whole shape exists to prevent
  comes straight back as a dead button. **Ask of any browser API: does it care
  *when* it was called, and does jsdom model that?** For anything gated on
  user activation — `window.open`, clipboard writes, fullscreen, autoplay,
  Web Share — the answer to the second half is no.

**If a behaviour depends on the platform, verify it in the platform.** A real
browser is what found every frontend defect above **except one**, and nothing
else could have: jsdom has no working `<dialog>`, no CSS cascade and no layout
at all, so the assertion that would catch one of these has nothing to read.
**The exception is the more interesting entry, not a footnote.** The
`window.open` bullet was found in code review, from the spec — someone knew
WebKit gates the call on the synchronous gesture stack and went looking — and
the walk on 2026-09-01 **confirmed the fix in a real browser** — the control
opened Telegram Desktop on the host and both the sign-up and sign-in paths
completed through it
(`docs/superpowers/plans/2026-09-01-telegram-sign-in-verification.md`). So it is
evidence for this pattern twice over, and the second half only closed once the
walk ran: the simulated environment could not see the defect, and could not
have confirmed the fix either. Reasoning a platform gap out of the spec is how
you find it; it is not how you confirm you closed it. Count the
bullets rather than trusting a number in this sentence — the list keeps
growing. And
when a service returns an error it did not log, stop debugging the code and
confirm you are talking to the process you think you are — an assumed
environment that is not the one running is the same trap as a simulated one
that cannot tell the truth, and every hypothesis about the code will be wrong
for as long as the premise is. A fix for one of those defects is also worth
a second look with your own eyes, not just a passing assertion: a green test
that only checks a class string is present says nothing about whether the
color that class names actually shows up against its own background — and
the grouped-sidebar bullet above is that exact claim happening again, on a
different component, after it was already written down here once.

### 4. Guards scoped to the wrong interval

Three times, a fix closed the reported sequence and left the class open. Each
time a reviewer found it by building a probe rather than reading the diff.

- Clearing an error **on mode change** could not cancel an error that arrived
  *after* the change — an abandoned request settling later rendered on a screen
  the user had already left.
- Disabling a control **while the mutation is pending** stopped too early,
  because `onSuccess` fired `invalidateQueries` without awaiting it. The mutation
  settled when the response arrived, not when the cache refreshed.
- Budget slice, Task 8 review found two instances of the same shape in
  `BudgetService`, in the same review pass. `History`'s `Closed` flag
  compared each row against the walk-back **anchor month** instead of
  today's real calendar month, so a page whose month picker sat on a past
  month while History was open would have mislabelled every row, including
  the anchor itself, as "so far" or vice versa — the interval the flag
  claims to cover ("is this month over yet") is anchored to the wrong clock.
  Separately, `DailyPaceOK` didn't check whether the *viewed* month was the
  *current* one at all: a budgeted future month with no spend yet has a
  positive `Remaining` and a full `DaysLeftInMonth`, so the pace card would
  have shown a false "on pace" for a month that hasn't started. Both fixed
  by anchoring the relevant comparison to `today` explicitly rather than to
  whatever month the caller happened to be walking, with a test for each
  that only distinguishes the two months when they actually differ.

**Ask what interval the guard actually covers, and what can arrive outside it.**

### 5. Silent partial success is worse than loud failure

- `HouseholdRepository.Update` accepted six fields and persisted four. A caller
  setting the other two got `nil` back.
- `PATCH /household` blanked every field the caller omitted — the *spec's own
  documented body* returned 500.
- Invite acceptance was three separate writes. A failure in the middle left an
  orphaned user holding the unique email index, so the invite could **never** be
  accepted, by anyone, ever.
- Creating a child was two writes with no transaction; because a child's email
  is NULL there is no constraint to fail loudly, so each retry silently created
  another orphan.
- A failed session revocation returned a bare error indistinguishable from "the
  change did not happen" — while the change *had* happened and the member's old
  session stayed live.
- The account edit modal forwarded the create mutation's request body shape
  to `PATCH /accounts/{id}` unchanged. The backend's real-patch convention
  treats a nil pointer as "leave this field alone", and JSON `null` decodes to
  that same nil — indistinguishable from the field being absent. The form
  models "Shared" as `null` (correct for *create*, where null and omission
  both default to shared), so selecting "Shared" while editing an owned
  account silently left the previous owner in place instead of clearing it —
  the PATCH answered 200 and changed nothing. Found by tracing the wire type
  through both sides of the same field, not by a failing test; fixed with a
  `null -> ""` translation on the update path only, and a dedicated test now
  asserts the PATCH body itself carries `""`.

- **A backup that restores every table and none of the roles — database
  browse, 2026-09-04.** Written before anyone has tripped it, because the day
  it bites is the worst possible day to discover it. `deploy/backup.sh` runs
  `pg_dump -U hearth -d hearth --no-owner --no-privileges`: **one database**,
  with the `GRANT`s stripped. `hearth_readonly` is a **role**, and roles live
  in the cluster, not in a database — `pg_dumpall --roles-only` is the tool
  that would capture one, and this product does not run it. So neither the
  role nor its grants are in any backup this product takes. A restore
  therefore *succeeds*: every table back, every monetary value exact, the
  product working — and the operator's database browse answering `503`
  against a role that no longer exists. There is no error at restore time,
  nothing in the log, and no reason for anyone to look. The failure arrives
  days or weeks later, on the day somebody opens the panel because something
  else is wrong. The mitigation is deliberately not a smarter backup: it is
  that `deploy/readonly-role.sql` is **idempotent**, so "run it again after
  every restore" is a whole instruction with no condition to evaluate, and
  it is written in `deploy/README.md`'s Restoring section rather than only in
  a spec — beside the command someone will actually be typing. What would
  have caught it sooner: **when a feature depends on state outside the thing
  that gets backed up, say so where the restore is documented** — asking
  "what does `pg_dump` not contain" is a question with a short, checkable
  answer, and nobody asked it until the role existed.

**Any two writes that must both happen need a transaction or a loud failure.**
And a function that accepts a field must persist it or refuse it — silently
keeping the old value is the same failure wearing a 200. **And a recovery
procedure that restores less than it appears to is the same defect at the
scale of a whole install** — the entry above is not about a repository
method, but it is the identical shape: an operation that reports success for
work it only partly did.

### 6. Enumeration oracles are rarely in the error code

The magic-link endpoint always returns 202 and sign-in always returns the same
countdown — and both still leaked, through side effects nobody thought of as
output.

- The locked branch skipped recording the attempt, so `lockedUntil` **froze**
  while the unknown-address path's deadline kept advancing. Watching which
  deadline moved told you whether the address was real.
- `Hasher.Verify` ran on only one of four branches, so argon2's deliberate cost —
  tens to hundreds of milliseconds — separated "real member with a password" from
  everything else.
- The mailer's error propagated only on the known-address branch, so a degraded
  relay turned a non-nil result into a discrete membership signal.
- Rate-limited requests were **faster** than unknown ones, because the count
  query joins through `users` and a stranger can never reach the limit.
- Sign-up's per-address limit counted rows in `signups`, but only the
  fresh-address branch ever wrote one — the registered-address branch answered
  `202` with an "already have an account" mail without touching the table its
  own limit was supposedly counted from. Four requests for one registered
  address sent four such mails, then forty, then four hundred: the same
  mailbox oracle this endpoint exists to close, expressed as unbounded mail
  *volume* rather than a discrete signal, on the one branch nobody thought to
  check was gated. The test that should have caught it passed anyway — its
  double let a caller force the counter's state directly (`setEmailCount`), a
  state no amount of real traffic sent through the real path could produce.

**Ask what a caller can measure, not what it is told:** status, body, timing,
number of round trips, whether an email arrived — and how much of it.

### 7. Floating versions break builds you did not touch

`air@latest` raised its Go minimum and broke the image. `goose@latest` sat one
line below it with identical exposure. `sqlc@latest` would have forced the whole
module to a newer Go than the Dockerfile pins.

**Pin tool versions and say why in a comment**, or someone will tidy them back
to `@latest`.

### 8. Configuration that lies

- `.env.example` claimed Compose read it. Compose hardcoded every value inline
  and read nothing.
- `SESSION_SECRET` was required at startup and used by nothing — implying
  sessions were signed when they are random and hashed.
- `APP_ENV` defaulted to `development`, so a production deploy that forgot to
  set it would silently serve non-`Secure` cookies.
- The seed's guard checked `APP_ENV` and said nothing about `DATABASE_URL`, so
  `APP_ENV=development` against a production database passed.
- Cookies are `Secure` in production while nginx listens on plain `:80` — as
  shipped, the browser never returns the session cookie.
- **A 30-day counter over a table a CLI flag deletes from — admin households,
  2026-09-02.** `/admin/households`' "sign-ups in the last 30 days" tile counts
  rows in `signups`, and `adminctl prune --older-than=N` deletes consumed and
  expired rows from that same table. `deploy/README.md` runs it at 30, so the
  two agree — by convention, not by construction. Set `--older-than=7` (the
  floor) and the tile silently under-reports for three weeks with nothing
  broken, nothing logged and no test red. The spec looked at coupling the two
  (a shared constant, or the query reading the retention setting) and chose to
  document instead: a counter that is at most one edge day short, and only if
  prune ran that morning, is not worth binding a CLI flag to a query constant
  (`2026-09-02-hearth-admin-households-design.md`, decision 10). That is a
  defensible trade, but it is only safe while it is *written where the flag is
  typed* — so the note lives beside the `prune` command in `deploy/README.md`,
  not only in the spec. What would have caught it sooner: **any counter over a
  window should name its retention dependency at the place the retention is
  set**, because the person shortening a prune window is never reading the
  query.

- **One `default:` arm answered three different failures with the same wrong
  advice — database browse, Task 7, 2026-09-04.** `main.go`'s boot switch
  refuses to start on `ErrReadOnlyMisconfigured` and, in its `default:` arm,
  left `Deps.AdminBrowse` nil and carried on. But nil is how the *handler*
  says `DB_BROWSE_NOT_CONFIGURED` — "you never set this variable" — and three
  distinct failures fell into that arm: the database being unreachable,
  `pgxpool.NewWithConfig` failing, and `assertCannotWrite`'s own privilege
  lookup erroring (which deliberately does *not* wrap the misconfiguration
  sentinel, so "I could not tell whether this role can write" degraded to
  "not configured" instead of refusing the boot). All three told the operator
  to set a variable that was already set — **on precisely the restore-day
  scenario the design was written for**: `.env` already carries the value,
  the role does not exist yet, and the person reading the message is
  rebuilding the box. The design's own decision table had always said that
  row must answer `503 DB_BROWSE_UNAVAILABLE`; the plan's wiring could not
  produce it, and the spec's prose never named a mechanism, so nothing
  contradicted anything until the code existed. Fixed with a second
  implementation of the port, `postgres.UnavailableBrowse`, which answers
  `ErrBrowseUnavailable` and carries the boot failure so the 503 can log
  *why*; the startup log's "enabled" line moved to `readonlyDB != nil`, since
  "the service is non-nil" had stopped meaning "a live pool was opened". A
  second half of the same defect: the failure log printed a pgx
  `ConnectError`, whose text is built from `user=` and `database=` only
  (`pgconn/errors.go`), so the string `DATABASE_READONLY_URL` appeared
  nowhere in the one line reporting the problem — no amount of wrapping would
  have added it, the message has to name the variable itself. What would have
  caught it sooner: **a `default:` arm is a claim that every remaining case
  wants the same answer** — enumerate them out loud before writing one, and
  if a nil value doubles as a user-facing message, it can only mean one
  thing.

- **`GRANT SELECT ON ALL TABLES` grants on the tables that exist when it
  runs — database browse, 2026-09-04.** Written whether or not anyone trips
  it, because the next person to grant a read-only role will write the
  incomplete version and nothing will tell them. `GRANT SELECT ON ALL TABLES
  IN SCHEMA public TO hearth_readonly` is a one-time act over the current
  catalogue, not a standing rule: the table the **next** migration creates is
  simply not covered. (Deliberately no migration number here. An earlier draft
  said `00015` while `readonly-role.sql`'s own comment said `00014` — two
  hypotheticals written in the same change, disagreeing, and both destined to
  go stale the day that migration lands. The invariant is "the next one",
  whatever it is numbered.) Nothing reports it — no error, no log line, no
  failing test; `information_schema` is filtered per role, so the browse's
  own "does this table exist" lookup answers *no such table*, which is the
  same answer a typo gets. A table silently missing from a list nobody counts
  is not a failure anyone notices. The fix is one more statement in the same
  file — `ALTER DEFAULT PRIVILEGES FOR ROLE hearth IN SCHEMA public GRANT
  SELECT ON TABLES TO hearth_readonly` — and it is easy to leave out because
  the incomplete version works perfectly on the day it is written. Note the
  `FOR ROLE hearth`: default privileges attach to the role that *creates* the
  object, so this covers tables goose creates as `hearth` and would not cover
  a table created by anyone else. What would have caught it sooner: **a
  grant that enumerates today's objects needs a rule for tomorrow's, or a
  test that adds an object and re-checks** — the browse's schema-driven
  redaction test is that shape for a different rule, written for exactly this
  reason.

**A config value that nothing reads is a lie. A guard that names one thing while
protecting another is worse. And a value that two places have to agree on, with
nothing making them agree, is a lie waiting for someone to change one of
them. And an error message that tells someone to fix a thing that is already
correct costs more than no message at all** — it spends the one minute they
had on the wrong file, on the day they can least afford it.

### 9. A DELETE scoped to a deliberately-nullable column spares exactly the rows that column's nullability exists to create

- `ClearFailures` is `DELETE FROM login_attempts WHERE household_id = $1 AND
  succeeded = false` — the right statement for clearing a *member's* failed
  attempts. But `household_id` is nullable on purpose, so an attempt against
  an address nobody recognises can still be recorded without revealing
  whether it exists (`migrations/00002_identity.sql`). `household_id = $1`
  cannot match `NULL`, so every row a stranger's failed guess ever created was
  deleted by nothing, ever, while every row a real member created was cleared
  on their next success. `login_attempts` was already unbounded before this
  slice touched it; `signups` (this slice's own new table) does not have the
  same defect only because nothing scopes its pruner to a household at all.
- Found by asking "which tables can a stranger grow at will?", not by a test —
  nothing failed, because nothing had ever asserted the unreachable rows got
  cleaned up. `adminctl prune` now covers both tables, with a floor that
  refuses to prune inside `domain.LockoutPolicy.Window` — deleting a row still
  inside it would clear a live lockout as a side effect of "cleanup".

- Telegram sign-in, whole-branch review, 2026-09-01: `users.email` is
  nullable for exactly the same reason as `login_attempts.household_id` above
  — a Telegram sign-up has no address, by design. `GetUserByEmail` is `WHERE
  email = $1` (`api/internal/adapter/postgres/queries/identity.sql`), and `$1`
  never matches `NULL`. The sign-up screen still collected a password for
  that account, `required minLength={12}` and all, so the form looked
  ordinary while the password it collected could never be checked against
  anything: `POST /auth/magic-link` has no address to send to, and `adminctl
  reset-password --email=` has no address to match either. Telegram became
  the *only* door into that household, with no operator recovery path, and
  nothing in the UI said so. The fix taken was the cheap honest one, not the
  architectural one: `SignUpCompleteScreen.tsx`'s Telegram copy now says
  plainly that Telegram is the only way in for now and the password will not
  work yet (letting a Telegram user attach an email later is tracked as ⬜ in
  `docs/FEATURE_TRACKER.md`, not built in this slice). Until that ships, the
  only recovery an operator has for a locked-out Telegram-only household is
  `make psql` and a hand-written `UPDATE users SET email = '...' WHERE id =
  '...'` — there is no `adminctl` command for it, because every existing one
  keys off the email this account does not have.

**When a column is nullable for a stated reason, ask what a filter on it
silently excludes.** A cleanup query scoped the same way a lookup query is
scoped inherits that query's blind spot for free. So does every other query
built for the row shape that has the column, run against the row shape that
doesn't.

### 10. Slicing a string by position, and "simple" case mapping, both assume one character is one unit

- `initialOf` took `displayName[:1]` — one *byte* — to get an avatar's first
  letter. Every ASCII name worked by accident; every multi-byte UTF-8 name
  (anything outside Latin, plus accented Latin) produced an invalid fragment
  that rendered as the replacement character, permanently, because there is no
  profile-edit endpoint to fix it. Invisible for as long as every name in the
  seed and every test fixture was ASCII.
- Fixing that to slice the first *rune* surfaced a second, narrower gap:
  `strings.ToUpper` is Go's *simple* case mapping, which leaves German `ß`
  alone rather than expanding it to `SS` — simple mapping is defined to be
  one-rune-to-one-rune, and `ß`→`SS` is not. The fix uses
  `golang.org/x/text/cases.Upper(language.Und)`, which performs full
  (language-independent) case mapping and can turn one rune into two, which is
  *why* `avatar_initial` widened from `char(1)` to `text` in the same
  migration — a one-rune expansion that `char(1)` would otherwise reject
  outright.
- Vision spec, Task 2: theme and description caps (`domain.MaxVisionThemeLen`,
  `MaxVisionDescriptionLen`) were first written as `len(v.Theme) > cap` —
  `len` on a Go string counts bytes. Hearth is built for a Singapore
  household, where a theme written in Chinese is not a hypothetical; every
  character in it is three bytes in UTF-8, so a 120-character promise
  silently became a 40-character one for exactly the households this
  product's own market makes ordinary, while every English fixture in the
  test suite — one byte per rune — never came near the boundary that would
  have shown it. Fixed to `utf8.RuneCountInString`, the same fix pattern 10's
  first bullet already made for `initialOf`, on the same category of field
  (user-authored text, capped for display, never validated against a
  non-Latin fixture until asked to be).

**Code that assumes "the first character" fits in one byte, or that upper-casing
never changes a string's length, is written for ASCII and will not say so.**
Both assumptions are invisible until a name that isn't ASCII reaches them.

### 11. Layered limits have asymmetric failure modes, and the one that fails silently and globally must never bind first

- Sign-up had a per-IP limit of 10/hour and a global daily mail ceiling of
  200. Each looked right alone. Together they did not compose: 10 × 24 = 240
  > 200, so a single IP, entirely inside its own hourly budget, could
  exhaust the global ceiling by itself. Once that ceiling trips, **every**
  sign-up on the platform silently answers `202` and mails nothing, for up
  to 24 hours, with nobody told.
- The same slice had no server-side email format validation, so
  `{"email":""}` still wrote a countable row into `signups` — a zero-cost
  way to spend either limit's budget, which makes any ceiling negotiable for
  free rather than merely tight.
- Found by a whole-branch review reading the two numbers together, after
  both limits had already shipped clean and reviewed on their own — not by
  a test. Neither per-task review could have caught it: each limit was
  correct within the scope of its own task, and the composition only exists
  across two separate commits.

**A per-IP `429` inconveniences one caller and announces itself. A global
ceiling tripping is invisible, platform-wide, and indistinguishable from
"nobody is signing up today."** The cheap, reversible limit must bite before
the expensive, silent one — and something must assert that relationship,
because two constants in different packages drift independently. The guard
here is a test, `TestSignUpRateLimitsCompose`, asserting
`signUpRequestsPerIPPerHour * 24 < SignupGlobalDailyLimit` against the live
constants — which is the entire reason `SignupGlobalDailyLimit` is exported
at all.

### 12. A sum over mixed currencies must convert before it adds

`domain.Money.Add` refuses to add two different currencies, by design.
Summing a household's accounts before converting each into the primary
currency would therefore not merely be wrong — it would fail outright, but
only once the household actually holds a second currency. A single-currency
household, or a test suite built only from single-currency fixtures, never
reaches the line that would catch it.

- `AccountService.Summary` converts each account into the household's
  primary currency through `Rate.Apply` *before* any `Add` — specified this
  way from the start, not discovered as a defect, because `domain.Money.Add`
  already refuses to add two currencies and the spec named the ordering
  explicitly. Mutation-checked by reordering the loop to sum raw balances
  first and convert once at the end: exactly `TestSummaryConvertsBeforeAdding`
  went red, with `domain.ErrCurrencyMismatch` surfacing through the same
  SGD/IDR pair the design's own seed household holds. Every single-currency
  test in the same file stayed green — which is the point. An ordering bug
  here is invisible until a household actually holds two currencies.

**A rule that only breaks under a second data point needs a test built from
that second data point.** Writing the mixed-currency test from the design's
own IDR account, rather than from the happy path, is what gave this rule
anything to prove itself against.

### 13. A verification walk scripted from the spec cannot catch the spec's own mistake

Transactions was walked 15 of 15, in a real browser, against a wiped
database — the strongest verification this project does. It still did not
survive the product owner's first real day-one use, because every one of
those 15 criteria was itself derived from the spec, and the spec was the
thing that was wrong, silent, or unexamined:

- **Wrong.** Transactions spec decision 6 defined `AccountView.Balance` as
  the opening balance plus every transaction dated strictly *after*
  `opening_balance_as_of`, on the theory that a balance typed "as of" a date
  already reflects that day's spending. The implementation matched the spec
  exactly. The theory fails on the most natural flow in the product: create
  an account today, with today as the default "as of," and log today's
  dinner. The transaction saved, the ledger marked it "before" the opening
  balance — wrong twice over, since it was same-day and the user never asked
  for the exclusion — and the balance and net worth sat still. Every
  day-one user hits this path; the walk never did, because nothing in the
  spec asked "what happens on the day the account is opened?" (fixed by
  decision 1 of `docs/superpowers/specs/2026-07-30-hearth-finance-fixes-design.md`:
  the opening balance is now the figure at the *start* of its day, so a
  same-day transaction counts — see the Money catalogue entry below and
  `FEATURE_TRACKER.md`'s Transactions row).
- **Silent.** `/money/transactions` existed and worked from the moment it
  shipped. Its only entry point was a small "See all" link beside "Recent
  transactions" on the Finances page — the design's own grouped sidebar
  (label plus a link per built page) was never built for it, because
  `Sidebar.tsx`'s own header comment had deliberately deferred grouping
  "until a space has more than one destination," and nothing re-read that
  comment once Transactions gave Money its second page. No criterion in the
  walk ever asked "how would a stranger discover this page?", because
  discoverability was never named as a thing to check — only the page's own
  contents were.
- **Unexamined.** The Add-account modal's "Visible to kids" copy read
  "Kayla & Ethan can see this account exists, not the balance" — the seeded
  household's own children, shipped as if it were generic product copy. The
  walk clicked through the modal, saw text, and moved on: nothing in a
  spec-derived checklist asks "whose names are these, and would they be
  right for a different household?" A stranger who signed up for their own
  account would meet someone else's children on their very first day.

None of the three would have shown up in a checklist built only from the
spec's own acceptance criteria, because in each case the spec (or the
absence of one) *was* the defect. The check that catches this class is
using the product the way a stranger would on day one — the household's own
most natural first flow — not only the criteria the spec happened to write
down. This is why `CLAUDE.md`'s definition of done now requires "test the
product in a real browser before calling it done," added the day this
round's spec was written; the sharper form of the rule — walk it as the
user, not as the spec — is this pattern, and item 4 of this file's own
closing checklist states it in operational terms.

The UX-repair round (M1) was that walk done properly — the product owner
opened the app as a stranger would — and it found two more of the same class,
neither of which any feature spec had a criterion for:

- **A placeholder page shipped as a live navigation destination, in the
  team's own planning vocabulary.** Marriage, Family and Overview each
  rendered nothing but the sentence "Arriving in slice N" — "slice" is a word
  from this project's plan documents, and it reached a customer's screen.
  Every one of those pages was *deliberately* built and *correctly* built:
  each slice's spec said "placeholder page for the unbuilt areas", and each
  task delivered exactly that. What no spec ever asked was what a household
  sees when it clicks one, which is a navigation row promising a page that
  does not exist, in a word only the people who built it understand. Two of
  the three — the unbuilt spaces — were deleted with their four routes
  (`110ab0a`); Overview keeps its placeholder because it is the landing page
  and cannot simply be dropped from the sidebar, and its replacement is its
  own plan. The rule that falls out: a placeholder is honest as the *inside*
  of something a household already has, and dishonest as a destination it is
  invited to visit. And the vocabulary check is free — grep rendered strings
  for the words that only exist in your planning documents (`slice`, `task`,
  `TODO`, `Phase`) before shipping.
- **A disabled control explained itself where nobody was looking.** With no
  accounts yet, the Transactions page disabled "+ Add transaction" — right,
  and documented: you cannot attach a transaction to an account that does not
  exist. But the explanation lived in the button's own area at the top right,
  while the eye was in the middle of the page reading the ledger's empty
  state, which said "Nothing logged yet." — an answer to a different question,
  and a dead end for a household whose actual next step was to create an
  account. The decision was reviewed; the *placement* was never checked
  against the empty state it belongs to, because a spec criterion asks "is the
  button disabled?" and a person asks "what do I do now?". Fixed by making the
  empty state itself read "Add an account first" with a link to Finances. When
  you disable a control, find where the user's eye actually is at that moment
  and put the reason and the way out there — the disabled control is where the
  answer is *filed*, not where it is *read*.

**A spec-derived verification walk is only as good as the spec.** Add one
criterion no spec item implies: do the thing a first-time user would
actually do, not the thing the spec remembered to ask about.

**The admin-households walk (2026-09-02) found the same shape inside a single
session, rather than needing a separate M1-style round.** Criterion 7 asked
only "does Clear restore the list?", and it did: `AdminHouseholdsPage.tsx` has
two Clear controls (the search form's own, and the one inside the "Nothing
matches" message), and both genuinely restore the household list. What the
criterion never asked was "what does the search box say afterward?" — the
results-area Clear left it showing the query that had just returned zero
results, while the table underneath showed everything: a real inconsistency
for whoever reads the screen rather than the network log. The walk passed the
letter of criterion 7 on the first read and only caught the defect by going
back and reading the page the way an operator would — the same discipline
named above, just applied a paragraph later in the same walk instead of in a
separate round. The lesson generalises past this one page: **local state that
mirrors a value the caller owns (here, `draft` mirroring the `q` prop the
route controls) must re-derive from that value on every change, not only on
mount** — `useState(q)` seeds once; a `useEffect` keyed on `q` is what keeps
it honest. The test that catches this is a prop change, not a click: a
component test that only clicks the Clear button and checks the URL never
exercises the case where `q` changes for a reason the component itself did
not initiate (a sibling control, the back button). Fixed the same day, in the
commit that recorded the walk
(`docs/superpowers/plans/2026-09-02-hearth-admin-households-verification.md`,
criterion 7's caveat).

The walk *scripts* themselves keep proving the adjacent, smaller point: every
walk so far has shipped with at least one criterion that could not be executed
as written. Accounts' criterion 12 said "sign in as Kayla," who is
credential-less by design. Sign-up's (run 2026-07-30) had two: criterion 11
asserts the per-IP limit's five-then-429 arithmetic, but the walk's own
criteria 3 and 10 had already spent two of the five requests — the limiter is
a fixed window per process, so the script's arithmetic only works from a fresh
API process, which the script never says; and criterion 13 counts "four
members" in Andreas's household where the seed deliberately creates three plus
Christine's *pending* invite, which the members list does not show. Each was
met by an interpreted path and fixed in the verification record, not the
product. When writing a walk script, dry-run its arithmetic against the state
the walk itself will have created by that step — a criterion that asserts a
counter must say what the counter has already counted.

### 14. Literal example data belongs to the seed, not the product

The design mockup was built around one imagined household — Andreas &
Christine, with children including Kayla and Ethan, Christine's accounts in
Indonesia. Twice, that household's specifics were typed directly into
rendered product copy rather than derived from whichever household is
actually signed in, and both times a comment at the site admitted it:

- `AccountModal.tsx`'s "Visible to kids" line hardcoded "Kayla & Ethan can
  see this account exists, not the balance" — literal design copy, correct
  only for the seeded household, sitting beside a member list the same
  modal already fetches for its owner dropdown. Fixed by a
  `limitedMembersLine` helper that names the household's actual limited
  members (joined with commas and "&"), falling back to generic copy
  ("Limited members can see this account exists, not the balance") when
  there are none.
- `CurrencyPanel.tsx`'s secondary-currency toggle hardcoded "For Christine's
  Indonesian accounts." Its own comment named the debt outright — "Literal
  design copy, true of this specific seeded household ... flagged in the
  report, not solved here" — and the debt was left exactly where the
  comment said it was until this round paid it off with a generic line,
  "Shown alongside the primary currency."

Both were harmless for as long as the seeded household was the only
household in the system — the comment on each said so, in the words pattern
1 already uses for this shape: a debt that comes due the day its stated
precondition stops holding. Self-serve sign-up falsified that precondition
for every real customer, and nothing about shipping self-serve sign-up
touched either file to notice. Neither was caught by a test — a `stubFetchRoutes`
member-list fixture built from the design's own names would have kept both
tests green regardless, the same way a fixture-with-no-case-to-discriminate
misses things elsewhere in this log. Found by the product owner asking "who
is Ethan?", then by grepping the built frontend bundle for every rendered
design-literal name (Andreas, Christine, Kayla, Ethan) to prove none of them
still ship — comments may keep the names, since a comment explaining *why*
a household exists in the seed is useful; a rendered string may not.

**A comment admitting "this is only true of the seed" is not a fix — it is
a marker for the fix that has not happened yet.** Grep for a design's own
proper nouns in rendered strings before calling a feature built from a
worked example done; a name that reads naturally in a mockup is a household
inventing itself as a person the moment it renders for someone else's family.

### 15. A capability nobody can reach is not shipped, however well it is tested

Goals shipped archive and restore end to end — a migration column, a
repository method with an idempotency contract, `GoalService.SetArchived`,
`POST /goals/{id}/archive`, a `useGoals.archiveGoal` mutation with its own
passing test (`useGoals.test.ts`, "archiveGoal POSTs /api/v1/goals/{id}/archive
then reloads"), a "Show archived" toggle on the Goals page, and a Restore
button on every archived card. `FEATURE_TRACKER.md` carried the row at ✅.
Every layer was real. **No screen ever called `archiveGoal`**, so no
household could reach the archived state the other half of the feature was
built to get out of. Found by the Task 18 browser walk at criterion 12,
which said "Archive Emergency fund" and had nothing to click.

Three things let it through, and each is worth checking for separately:

- **The plan never assigned the control to a task.** Task 11's brief lists
  the archived view "each marked '(archived)' with a Restore action"; Task
  12's field list is name, target, currency, date, starting balance,
  planned monthly. Neither mentions Archive, so no task's tests were wrong
  — the work item simply did not exist. This is the second plan gap on this
  branch: `d1c7248` wired `GoalModal` into `GoalsPage` for the same reason.
- **The hook test proved the capability, not the reach.** A test that
  mounts `useGoals` and calls `archiveGoal` is a true statement about the
  hook and says nothing about whether any component ever does. The cheap
  mechanical check: for each mutation a hook returns, grep the source
  (excluding the hook and its own tests) for a caller. `archiveGoal` was
  the only one of seven with none — thirty seconds, run once per feature.
- **The tracker row was ticked from the backend up.** "Archive and restore
  a goal ✅" was written while archive had no UI, because everything behind
  it existed. A row describes what a household can *do*, not what the stack
  can serve.

Fixed by an Archive button on every live card, mirroring `AccountRow`'s own
either/or (Archive on live, Restore on archived, never both), no
`window.confirm` since archiving is reversible from the archived view. Two
new mutation-checked tests in `GoalsPage.test.tsx` pin it: the button POSTs
`/goals/{id}/archive` and the list refetches, and an archived card offers
Restore and *not* Archive.

**The trap inside the fix is the one `docs/HANDOVER.md` §1 already records.**
`GoalCard`'s root is `role="button"` with both `onClick` and `onKeyDown`, so
a nested button must stop *both* — a real browser fires a separate `click`
after the Enter keydown has already bubbled, so stopping only the click
leaves Enter archiving the goal *and* opening the edit modal behind the
disappearing card. `fireEvent.click` never presses a key, so no unit test
here can see that; the re-walk pressed Enter in a real browser and checked
no modal opened. "Add contribution" carries the same pair for the same
reason, and its comment is where this was learned the first time.

**Second instance, 2026-08-16 — the four notification rows.** Same shape,
found by reading rather than walking, while answering "what's next" for
Marriage. `Notifications — bill due reminders`, `— overspend alerts`,
`— monthly retro reminder` and `— weekly family digest` were all ✅. Everything
under them is real: the `notification_preferences` table, the repository, `GET`
and `PATCH /household/notifications`, `NotificationsPanel.tsx`, tests on all of
it. **Nothing sends any of them.** `usecase.Mailer` has exactly three methods —
`SendMagicLink`, `SendInvite`, `SendSignupLink`/`SendSignupForExistingAccount` —
no caller anywhere reads a household's preferences in order to mail something,
and nothing in this codebase runs on a clock at all (the only cron in the
project is the box's nightly backup). The design's copy is a delivery promise,
not a switch: "Bill due reminders (3 days before)".

What is new here, beyond confirming the pattern:

- **A toggle is a plausible-looking terminus.** Goals' gap was a missing
  button — an absence, visible the moment someone looked for it. This one is
  a present, working, satisfying control: it moves, it persists, it survives a
  reload. Nothing about the screen suggests the far end is missing, which is
  why four rows sat wrong for weeks while every walk passed.
- **The mechanical check generalises.** For Goals it was "for each mutation a
  hook returns, grep for a caller outside the hook". The same question asked of
  a *stored preference* is: grep for a reader of that column outside the code
  that writes and returns it. Four columns, no readers. Thirty seconds again.
- **The headline number hid it.** The tracker's "73 of 103 built or partly
  built" counts ✅ and 🟡 identically, so correcting four rows moved no summary
  figure at all. A count that treats "done" and "partly done" as one bucket
  cannot report this class of error.

Corrected in `docs/FEATURE_TRACKER.md` to 🟡 with the gap named on each row
(Household settings 15/4/2 → 11/8/2, totals 60/13/28/2 → 56/17/28/2, no row
added or removed). Deliberately **not** fixed in code: building this product's
first scheduler is a spec of its own, and Budget decision 1, Goals decision 4
and Bills decision 3 have each already refused to invent one inside a feature
that merely wanted it.

**Third instance, 2026-08-16 — the "+ Add an action" composer, and this time
the gap was in the plan, not in any implementation.** The design's modal
draws "+ Add an action & assign it to one of you" as its own control,
`POST /retros/{month}/actions` existed from Task 8, and
`RetroActionRepository.Add` from Task 6 — every layer was real, the same
shape as the two instances above. What was missing was a piece neither of
those two had to name: **no task in the plan ever assigned the composer to
anyone.** Task 14 built the carry-over control, which also calls
`POST /retros/{month}/actions` (with `carriedFrom` set) — so by the time
Task 14 landed, that endpoint had a caller, its tests passed, and nothing
about the running suite suggested anything was missing. A household could
carry an old action into the new month and could never write a brand-new
one; the modal's own free-standing composer simply did not exist. Found by
the Task 14 implementer itself, reading the design's mockup against what the
task actually built, not by review or a walk. Ruled in-round rather than
deferred: Task 14 already owned the modal's actions block, and the
alternative was shipping a feature whose central control — the one the
design names first — did nothing. **The mechanical check from the two
instances above still applies, but it would have found nothing here**,
because `addAction` did have a caller — just not the one the design
promised. What this instance adds to the pattern: a plan can give one route
two different UI purposes and never notice that neither task actually built
the plainer of the two, because the endpoint's test coverage looks identical
either way.

**Fourth instance, 2026-08-17, found while writing this very tracker
correction — the round that documents the third instance turns out to
contain a fourth nobody had noticed.** `useRetro.ts` exposes `discardDraft`,
backed by a real migration, a mutation-checked `RetroRepository.DeleteDraft`
(`WHERE completed_at IS NULL`), `DELETE /retros/{month}`, and the hook's own
passing test. Task 16's brief, written from the plan, said to mark "Delete a
draft retro" ✅ alongside the carry-over row. Running the mechanical check
first — `grep -rl discardDraft web/src/features/marriage/`, excluding the
hook and its own test file — returned nothing: no component calls it,
anywhere. The same grep against every other mutation `useRetro.ts` and
`useRetros.ts` expose found a second, unpromised instance in the same file:
`removeAction` (`DELETE /retros/{month}/actions/{id}`) has no caller either,
though no design mockup or task brief ever named a "delete an action"
control, so there is no tracker row for it to falsify. `docs/FEATURE_TRACKER.md`
recorded "Delete a draft retro" as 🟡, not the ✅ the brief asked for — CLAUDE.md's
"do not mark anything ✅ that you cannot point at working code for" outranks a
brief's row-by-row instruction, the same way it outranked the plan for the
third instance above. **Closed the same round**: `RetroModal.tsx` was given a
Discard-draft control in `4d719b8`, `discardDraft` now has a real caller, and
the row reads ✅. `removeAction` stays uncalled — deliberately, since no
mockup or brief ever named the control it would back. **A docs task that reconciles a tracker is exactly the
moment this pattern re-checks itself for free**: the mechanical grep costs
thirty seconds per hook and was already due, since nobody had run it since
Task 9 built the hook two tasks before the composer gap even opened.

**A fourth instance, and this one had no missing caller at all — the platform
was standing in front of the caller.** `TransactionModal` writes its own
validation messages, sets them from `handleSubmit` via `describeAmountError`,
and renders them in a `role="alert"` paragraph beside the field. All of it
real, all of it tested. Submitting the form empty showed none of it: `noValidate`
appears in **zero** of this app's fifteen forms, so the browser's own
constraint validation refuses the submit before the `submit` event fires and
`handleSubmit` never runs. What a household actually saw was Chrome's bubble,
in Chrome's words, with Chrome's blue ring, on a form whose own message for
that exact case was already written. The file said so itself, in a comment
thirteen lines from the fix: "a literally empty required field never reaches
this far at all".

Two things generalise. **A capability can be unreachable because something
else answers first, not only because nothing calls it** — the mechanical
check for the missing-caller case (grep each hook's mutations for a caller)
would have found nothing wrong here, because the caller exists and is
correct. And **removing an interception exposes every case it was silently
covering**: date, description and account carried `required` and no check in
`handleSubmit`, because nothing had ever reached it with one of them empty.
Adding `noValidate` without adding those three would have traded Chrome's
bubble for a 422 from the API. The remaining fourteen forms share the shape
and are named as a follow-up rather than swept in silently.

**A sixth instance, Vision spec's task 12, and this time the call site was
wired from the start — the gap was that nothing proved it did anything.**
`VisionPage.tsx`'s `onEdit` had three `onClick={onEdit}` call sites from
task 11 onward — the header's Edit vision button, MilestoneGrid's "+ Add
milestone" tile, and the empty state's own call to action — every one of
them a real, rendered, correctly-wired button. `onEdit` itself was a
documented no-op placeholder until task 12 built the modal, at which point
all three became live in the same commit that replaced the handler. Nothing
before task 12 had ever asserted that clicking any of the three actually
opened anything, because there was nothing to open — a review finding on
task 11 named this explicitly and deferred it, rather than letting a green
suite imply the wiring was proven. The empty state's own call to action is
the sharper case of the three: it is the first thing a brand-new household
with no vision yet sees, and it had never been clicked in a test at any
point in this feature's history. Task 12 added one test per call site
(`VisionPage.test.tsx`, "the Edit-vision modal's three entry points") and
mutation-checked the one with no prior coverage: deleting `onClick={onEdit}`
from the empty-state button turned exactly that one test red and no other,
confirming each of the three is independently covered rather than one test
accidentally exercising all three through a shared render path. **The
mechanical check from the earlier instances above does not generalise to
this shape** — grepping for a caller of `onEdit` would have found three,
correctly, and said nothing about whether any of them produced an
observable effect once the handler stopped being a no-op. What actually
catches it: for a handler wired to more than one control, assert the effect
of each control separately, and mutate one call site's own wiring in
isolation to confirm its test — not a sibling's — is what goes red.

**A seventh instance, the admin-surface browser walk (2026-09-02), and this
time the code was correct and the test was correct — a green suite proved
the control worked, and it still shipped unusable.** `/admin/flags`'s only
interactive control, a segmented On/Off toggle, was real and correctly
tested: `AdminFlagsPage.test.tsx` asserts the right `PUT` fires and checks
`aria-pressed` on the On and Off buttons for exactly the state each row is
in (the one segment with no button behind it — "Default," meaning no
override exists — is asserted by a background-colour class instead, since
there is nothing to press back to it). Every one of those assertions was
correct and stayed green. Driving the actual screen found something none of
them could see: 12px text in a muted grey on a transparent ground inside a
hairline border, right-aligned roughly 1900px from its own label at a 2246px
viewport. It did not register on a first read of the screen at all, and its
presence had to be confirmed through the accessibility tree and
`getBoundingClientRect` — not through looking at the page a household, or an
operator, actually would. No unit test could have caught this: a jsdom
render has no viewport and no layout, so `aria-pressed` being correct and a
control being findable are two entirely different claims, and only one of
them has a DOM API to assert against. The same walk found a sharper
case of the pattern this section is named for, one click away: there is no
control anywhere on the screen to *create* a household override — only a
global toggle and a "Remove" on overrides that already exist — so the
per-household targeting the whole two-layer flag model exists to provide is,
today, reachable only by a hand-written `PUT` (`docs/FEATURE_TRACKER.md` §9,
[ADR 5](adr/0005-platform-admin-authorization.md)). **This is the standing
argument for driving the real page rather than trusting the suite one more
time, with fresh evidence**: nothing here was a failing assertion waiting to
be written, because there was no code path to assert against in the first
case and no coordinate system to assert legibility in for the second.

### 16. A claim about the code is not evidence until someone checks it against the code

- **The net worth trend's plan, 2026-08-19.** `docs/FEATURE_TRACKER.md`'s own
  row said the twelve-month trend "needs balance snapshots: a second table,
  and a separate decision about when a snapshot gets written (nightly? on
  every balance change? on read?)." It was false the day it was written:
  every balance in this product has always been derived from the
  transactions ledger on read — `ListAccounts`'s own query, unchanged since
  Transactions shipped — and the trend is the identical idea walked back
  twelve months. `AccountRepository.MonthlyMovements` (one new query) plus
  `AccountService.trend()` (`api/internal/usecase/networth_trend.go`)
  recompute all twelve bars on every `GET /accounts`; no table, no
  scheduler, no decision to defer. A shippable feature sat behind an
  imagined migration, deferred out of this round's own spec, because
  nobody read the query before believing the note about it. What would have
  caught it sooner: a document that states an implementation constraint
  should cite the code that imposes it, so a later reader can check the
  citation instead of trusting the claim — the same discipline a line-number
  citation owes a reader, below.
- **The same round, a smaller instance of the identical habit.** Task 3's
  review left two comments citing `account.go:167` for the one-day
  opening-date slack a household in UTC+8 needs; the actual check had
  already moved to line 177 by the time either comment was written. The
  wrong number was corrected twice before it stuck in the tree — once in
  Task 3's own fix round, and again when Task 4 touched the same file and
  reintroduced it while editing nearby. A bare line number is a claim with
  no way to check itself once the file around it moves; citing the
  function, symbol or query name instead survives the code moving under it.
- **The same round, a third instance, caught *while writing up the first
  one* — a claim about a design mockup this time, not a query.** The
  "Net worth card" row said Overview's gap was that "the design's card
  also carries a full assets/liabilities breakdown, which Overview's own
  card does not draw." Restated, not re-checked: the sentence was already
  in the tracker before this round touched it, and the fix round that
  corrected the trend row's own false constraint (the bullet above)
  reworded this exact sentence right next to it, in the same paragraph,
  without opening `design/Household Dashboard.dc.html` to look. A second
  review round did look, and it does not say that: the Overview net worth
  tile draws three stacked lines — label, figure, change — in every
  iteration of the file, and the only "Assets & liabilities" block the
  design contains anywhere is a separate sibling card the Finances screen
  alone draws, already built as its own row in the same table. There was
  no gap to name — the row's own next clause had already half-admitted it
  ("was never meant to… stays Finances-only by design") without anyone
  following that sentence to where it actually led. **What makes this
  instance worth its own bullet, not a footnote on the one above:** the
  person who wrote the corrected paragraph was, in that same paragraph,
  actively practising "cite the code, don't trust the claim" against a
  *different* sentence one row up, and still let this one through
  unchecked — proof that naming the discipline is not the same as
  applying it to everything in reach, especially the sentence sitting
  right next to the one just fixed.
- **A fourth instance, found in the net worth trend branch's own final
  review, inside the very paragraph written to warn about this.** The
  tracker cited a bare line range in `design/Household Dashboard.dc.html`
  for the Finances "Assets & liabilities" card, and the range was wrong at
  both ends -- a range nobody had re-opened the file to check since it was
  written. The fix was to stop citing a range at all: name the block ("the
  *Assets & liabilities* card on the Finances page") instead of its line
  numbers, the same prescription this paragraph already gives for
  `account.go:167` drifting to 177 -- a citation by name survives the file
  moving under it, and cannot itself go stale the way a number can. **And
  the correction shipped with wrong numbers too**, inherited from a review
  that had not opened the file either and carried forward without anyone
  re-deriving them -- pattern 16 firing a third time inside the very act of
  writing it down, which is the actual reason the rule is "name the block,"
  not "cite carefully." A citation inside the passage arguing that
  citations need checking is not exempt from needing to be checked, and
  neither is the fix for it.
- **A fifth instance, UI-polish round, and the first where the drifted claim
  lived inside the rule's own comment rather than in a citation of one.**
  Two comments each asserted a rule the code no longer followed. One sat in
  `web/src/index.css`, on the `.tabular` rule itself, restating the rule in
  words that had fallen out of step with what the rule actually says by the
  time the round finished correcting it (see pattern 1's tabular-nums entry
  above). The other, in `GoalCard.tsx`, cited "the placement rule excludes
  dates by name" — a clause that was never written anywhere in this project;
  grepping for it after the fact found nothing to find. Both are the same
  failure as a stale line number wearing different clothes: a comment that
  restates a rule in its own words, rather than pointing at the rule's one
  real statement, has nothing tying it to that statement once either side
  changes, so the paraphrase has no way to notice it has drifted. The
  drift-proof form is the one this pattern already prescribes for a line
  citation — name the single place the rule actually lives and point at it,
  rather than repeating it in prose that can only ever be checked by going
  and finding that place anyway.
- **A sixth instance, the admin-surface branch (2026-09-02), and the first
  time this shipped seven times on one branch rather than once.** Every one
  of the seven was a doc comment asserting something checkable — an
  ordering, a count, a list of callers — that the code had already moved out
  from under: a middleware comment in `auditAdmin` stated the *inverse* of
  the CSRF ordering after a review round changed it; a route-walk floor
  comment credited the wrong mechanism for a number it had just raised; two
  comments enumerating `buildMeResponse`'s callers undercounted them ("three
  of four" instead of four of five — `handleCompleteSignUp` missed both
  times it was counted); one on `Deps.Admin` in `router.go` said a nil
  dependency broke only the admin routes, when by that point `requireSession`
  called it on every authenticated request and a nil value panicked the
  *whole* authenticated surface — household, spaces, accounts, money,
  everything `requireSession` gates, not only `/admin`; one on `Sidebar.tsx`'s
  new Admin link said the server refuses a non-admin with `403`, when it
  answers `404` — and `useFeature.ts`, a second file in the very same commit,
  had the right answer sitting a few lines away; and one in `client.ts` named
  `requireSession` as the only middleware that can report a dead session, when
  four call sites can. Every one of the seven passed `make test` clean,
  because every one was prose, and prose is the one artifact in this
  repository no test holds to account. **The fix that stuck was not fixing
  the number, it was deleting the number:** "three of four" became "every
  caller except `handleMe`," with every comment needing the caller list
  pointed at the one function that now owns it, rather than five separate
  places repeating a count that would only drift again. State the invariant,
  never the enumeration — a property survives the next caller; a tally does
  not.
- **A seventh instance, same branch, where the drifted comment did not just
  mislead a reader — it hid a defect for the row's entire life, silently,
  behind a green suite.** `auditAdmin`'s own doc comment said `Detail` carries
  the matched route's URL parameters. It never could: chi's `FindRoute` — the
  step that actually populates a route's `{key}`/`{householdID}` values —
  runs inside `routeHTTP`, *after* every subtree middleware including this
  one, so every admin audit row this branch ever wrote carried `target: ""`
  and `detail: {"*": ""}`. The one test covering the write asserted a row
  *count*, never a value, and the comment said the field was populated, so
  nobody went looking. Found only when a later task added the first admin
  route with a real URL parameter in it and a reviewer, checking the claim
  against chi's own source rather than the comment, found it false. The fix
  matches the sixth instance's shape exactly: stop asserting a value that
  cannot exist at that point in the chain, rather than trying to keep the
  assertion current — `Target` became the full request path (which already
  contains every value those parameters would have held), `Detail` stays an
  empty object, and the comment now explains *why* parameters can't be read
  there instead of claiming they are.
- **An eighth instance, same branch, found the same way as the seventh --
  and fixed in this branch's own final wave, which is also where a second
  site of the identical instance turned up.** `migrations/00012_admin.sql`'s
  own comment on `admin_audit_log` said `target`, `detail` and `ip` "default
  rather than being NOT NULL without one, because auditAdmin writes from
  middleware where there is not always a target to name." That was true when
  the migration was written and stopped being true the moment the seventh
  instance's fix landed: `Target` is now always `r.URL.Path`, never empty, so
  the reasoning the comment gave for the column's own nullability was stale
  one file away from the code that made it stale. Only that migration
  comment was known when this entry was first drafted. The whole-branch
  review that closed out the fix wave found a second copy of the identical
  claim forty lines into `admin_schema_test.go`, on
  `TestAuditRowsNeedOnlyAnActorAndAnAction`'s doc comment -- worse than the
  first, because it also mis-stated what its own test proves (that the
  schema's defaults are reachable, never a fact about the middleware).
  Fixing the named site alone, which is what this entry originally recorded
  as the plan, would have fixed the instance and left the class alive one
  file over. Both now say what is actually true instead: the columns default
  so a writer is never forced to invent a value it does not have, not
  because of anything about one caller's current behaviour -- the durable,
  invariant-first form this pattern's own closing paragraph already
  prescribes. This is exactly the situation the repo's own
  `hunting-sibling-defects` skill exists for: a fix that stops at the named
  instance instead of asking where else the identical mistake was made.
- **A ninth instance, the same fix wave, caught while fixing the sixth
  instance's own siblings rather than while looking for a new one.** Two
  route-walk floor comments in `auth_api_test.go`
  (`TestEveryProtectedRouteRejectsAnUnauthenticatedCaller` and
  `TestEveryMutatingRouteRequiresCSRF`) read "62, not the pre-Task-8 18" and
  "44, not the pre-Task-8 11", naming a single task as the cause of the
  drift and a specific number as what the floor stood at beforehand.
  Checking the claim against the code it named -- rather than the fix
  wave's own brief, which handed down a further wrong number for the same
  passage, asserting the code had read `checked < 17` immediately
  beforehand -- a direct read of the parent commit's blob
  (`git show <parent>:api/internal/adapter/http/auth_api_test.go`) showed
  the floor was in fact `18`, and running the walk itself against the
  actual code from immediately before the named task's own commit returned
  real counts of `59` and `41`, not `18` and `11`: the drift had accumulated
  over several earlier tasks, not the one the comment named. The corrected
  comments drop the task attribution and the specific historical tally
  altogether, stating only the invariant the earlier version buried inside
  a history lesson: the floor is the walk's own re-measured output, never a
  number to nudge forward by however much the latest task seems to have
  added. What makes this one worth its own bullet rather than a footnote on
  the eighth: the wrong number arrived inside this very fix wave's own
  instructions for closing out Pattern 16, proving yet again that a claim
  about the code carries no more authority for having been written down to
  correct a previous one.
- **A tenth instance, the admin households branch (2026-09-02) — a doc
  comment that named the wrong failure mode for the guard it sat on, caught
  by mutating the guard away.** `handleAdminHousehold`'s malformed-id check
  arrived from the plan with the comment "so a malformed one answers the same
  404 a missing household does rather than the zero-UUID 500 the flag override
  routes still carry (`ADMIN_SURFACE_HANDOVER.md`, 'Known, deferred')".
  Two things were checked and both contradicted it. Commenting the guard out
  left `TestAdminHouseholdUnknownAndMalformedIDsAre404` **green**: on a
  `SELECT`, `postgres/convert.go`'s lenient `uuid()` helper turns an
  unparseable id into the zero `pgtype.UUID{}`, the query matches no row, and
  `translate` answers `domain.ErrNotFound` — the same 404, guard or no guard.
  And the 500 the comment pointed at is a *write* path (a foreign-key
  violation on `SetHouseholdFlag`'s `ON CONFLICT`), a failure a read cannot
  reach at all. The guard is still worth having, for two reasons the
  replacement comment now states instead: it fails closed on a value nobody in
  this system constructed, and it refuses before the service, the repository
  and a database round trip. **Corrected on the branch (`617db73`), not
  shipped wrong** — but it was in the tree for one commit purely because it
  read plausibly. The replacement carried a smaller version of the same
  defect for a while: it said the guard "skips three SQL round-trips" when
  `AdminDirectoryRepo.Household` short-circuits on its first query, so it
  skips one — corrected in the households-and-metrics browser-walk fix commit
  (2026-09-02, Task 11), the same commit this entry itself was written in. A
  correction written to close out an unverified claim is not itself
  verified — twice, in this case, before it stuck.
- **An eleventh instance, the outbound message inspector spec, 2026-09-04 —
  the one entry here where the discipline caught the trap before any code
  existed to drift, rather than after.** Mailpit's
  `GET /api/v1/message/{id}/link-check` reads, from its name and its place
  next to `GET /api/v1/message/{id}`, like the obvious way to answer "is
  this link still good" for the exact screen this spec was designing.
  Reading Mailpit v1.30.5's actual source instead of trusting what the name
  implies (`internal/linkcheck/status.go`) found it issues a real HTTP
  request — a `doHead` — to every URL it reports on, and every URL in a
  Hearth email is a live single-use token on a public host: calling it
  would spend the very links the screen exists to hand out. The same read
  of the same source turned up a second fact nobody had gone looking for:
  `GET /api/v1/message/{id}`, the endpoint the design keeps, marks the
  message read in Mailpit's own store as a side effect of a plain GET —
  accepted rather than avoided (decision 15; avoiding it means parsing raw
  MIME by hand) but written down at the point someone would notice the
  discrepancy, rather than left for them to find by comparing Mailpit's
  unread count before and after. Nothing here would have gone red: no test
  was failing, no code called either endpoint yet, and the design would
  have read as clean without the source ever being opened. What holds the
  rule in the code that shipped is not the discovery itself, it is an
  adapter test asserting only two upstream paths are ever requested — it
  fails on any third path, so a later refactor reaching for `link-check`
  because it looks like a gift fails a test instead of shipping, which is
  where the ten instances above eventually landed.

- **A comment crediting a vendor library with a guarantee it does not
  provide — database browse, Task 4, 2026-09-04.** The read-only pool's
  self-check runs in `pgxpool`'s `AfterConnect` hook and returns
  `ErrReadOnlyMisconfigured`; `main.go` matches that sentinel with
  `errors.Is` to decide whether to refuse the boot. The brief's comment
  explained why that works: "the error travels out through pgxpool's own
  wrapping of `AfterConnect`". The implementer first reported the same thing,
  then went and read the source — pinned `pgx v5.10.0` and `puddle v2.2.2` —
  and corrected itself. **pgxpool wraps nothing.** The error is passed
  through raw at every hop from the `Constructor` closure to
  `initResourceValue` to `Pool.Ping`; there is no `fmt.Errorf` anywhere on
  that path. `errors.Is` succeeds only because the two `%w` wraps inside
  `readonly_pool.go` *are* the entire chain. The conclusion is unchanged and
  the guarantee is actually **stronger** than the comment claimed — it
  depends on this file alone and cannot be broken by a pgxpool upgrade — but
  the stated reason was false, and a future reader trusting it would have
  believed a dependency was holding something up for them. Worth being
  equally precise about the one place pgx *does* embed a connection string,
  because the careless version of that sentence is the same defect again:
  `ParseConfigError` carries the DSN, and it **does** reach a log line —
  `OpenReadOnly` wraps it, `run()` returns it, and `main()` prints it with
  `slog.Error("fatal", …)`. What stops the leak is not that the error is
  swallowed; it is that `ParseConfigError.Error()` **redacts the password
  itself**, in pgx's own code. (The sentinel that makes that path refuse the
  boot, `ErrReadOnlyMisconfigured`, is added by `readonly_pool.go`'s own
  `%w` — pgx knows nothing about it. Same trap as the paragraph above:
  crediting the library with something this file does.) What would have
  caught it sooner: **an error-handling comment that credits a library is a
  claim about that library's source** — ten minutes reading it is the check,
  and it is the same check as reading a query before believing a note about
  it.

- **A comment whose motivating example the feature makes impossible to
  observe — database browse, Task 11's browser walk, 2026-09-04. The test
  sitting next to it had the truth the whole time.** `domain.NullCell` and
  the screen's `Legend()` both explained why `«null»` needs to exist as a
  separate marker from `«redacted»`, and both reached for the same example:
  "the difference is sometimes the bug being investigated (`users.password_hash`
  is NULL for a member who has only ever signed in with a magic link)". The
  first half is true — `password_hash` really is NULL for the household's
  two children. The implication is false: `ColumnIsRedacted` matches the
  `_hash` suffix, so `BrowseRepo.Rows` substitutes the marker **into the
  `SELECT` list** rather than fetching the column, and a NULL in it renders
  `«redacted»`. Redaction wins over nullness unconditionally, by
  construction. The one example offered for what `«null»` is *for* is the
  one column in this schema where it can never appear — confirmed on screen
  during the walk, where both children show `«redacted»`.
  **Nothing was broken and no test was failing**, which is exactly why it
  survived a spec, a plan, an implementation and a code review: the code was
  right, only the sentence explaining it was wrong, and a wrong sentence
  compiles. What makes this one sharper than the ten instances above is
  where the truth already lived. `TestRowsDistinguishesNullFromEmpty`
  asserts all three facts correctly, including, at
  `browse_repo_test.go:142`, that a NULL in a redacted column renders
  `«redacted»`. **The test knew; the prose beside it drifted.** So the fix
  needed no new test — the corrected claim was already pinned — and the
  honest move was to mutation-check the existing assertion instead of adding
  a ceremonial one: making the old comment's implicit claim true (a `CASE
  WHEN … IS NULL THEN NullCell ELSE RedactedCell END`) turned it red with
  "a NULL in a redacted column rendered as `«null»`, want `«redacted»`".
  What would have caught it sooner: **an example in a comment is a claim
  that can be run.** When a comment says "for instance, column X shows
  marker Y", open the screen and look at column X — this one costs one page
  view, and the walk that finally did it took under a minute. The
  corollary is worth as much: when a test near a comment asserts something
  the comment contradicts, the test is the one to believe, and the
  disagreement is the defect report.
  The same walk also found the *brief* wrong in the same way — criterion 7
  told the walker to use this very column — which is why walks are scripted
  from criteria and not trusted as oracles (see pattern 13).
- **The same walk's own correction to that brief was itself an unchecked
  claim, and it was filed under this very pattern before anyone ran it.**
  The verification file went on to say the brief was wrong a *second* time:
  that criterion 13's `insert into households (id, name)` omits a `NOT NULL`
  column, so it "would have answered a `NOT NULL` violation — a refusal that
  proves nothing about the role", and that only the three-column form was
  runnable. Review ran the brief's exact statement as `hearth_readonly`:
  `ERROR:  cannot execute INSERT in a read-only transaction`. It produces the
  criterion's own refusal, and nothing was written. **The brief was right and
  the correction was wrong** — a false claim about verification, written into
  the entry about false claims, in the same commit that added the entry.
  What the mistake actually was, and this is the reusable part: **both of
  this role's guards run before any constraint is checked.**
  `PreventCommandIfReadOnly` (for `default_transaction_read_only`) and
  `ExecCheckPermissions` (for the `GRANT`) both fire in `ExecutorStart`,
  while `NOT NULL` is `ExecConstraints` during `ExecutorRun` — the statement
  is refused before a row is ever built, so the missing column is never
  reached. Checked three ways rather than asserted: as the *owner*, where
  neither guard applies, the identical statement does give
  `null value in column "family_name" … violates not-null constraint`; and
  with only the read-only guard bypassed (`BEGIN; SET TRANSACTION READ
  WRITE;`) the same two-column form still gives
  `permission denied for table households`. So the brief's SQL proves both
  guards independently, exactly as the three-column form does.
  **The conflation was between two failures at two different stages**: an
  unknown *column name* really does beat both guards, because it fails at
  parse analysis before the executor starts at all
  (`column "nosuchcolumn" of relation "households" does not exist`), whereas
  an *omitted* NOT NULL column is a runtime check the guards never let the
  statement reach. Plausible-sounding, adjacent, and opposite. What would
  have caught it sooner is embarrassingly cheap and is this pattern's whole
  thesis: **the claim was that a specific SQL statement fails a specific
  way, and running it takes one command.** A correction is not exempt from
  the rule it is correcting — this is now the third time in this pattern
  (see the `account.go:167` line number and the `AdminHouseholdPage`
  round-trip count above) that a fix written to close out an unverified
  claim shipped unverified itself.

- **Two rules in one file whose examples contradicted each other — database
  browse, Task 5, 2026-09-04.** The redaction predicate matches by column
  type (`bytea`) and by name (`_hash`, `_secret`, anything containing
  `password`). The plan's adapter test then chose `users.password_hash` as
  its demonstration that a `NULL` renders `«null»`. That column matches the
  name rule twice over, so it can only ever render `«redacted»` — **the
  assertion the plan wrote could never have passed**, whatever the code did.
  Both rules were written in the same document, days apart, and neither was
  checked against the other. Replaced with `users.email` (`citext`,
  genuinely nullable — a Telegram-only account has none) for `NULL` and
  `display_name` for the empty string, and the test additionally now asserts
  the stronger property: `password_hash` renders `«redacted»` *even when it
  is NULL*, which is the case where redaction and absence could have been
  confused. What would have caught it sooner: **when a document defines a
  rule and later picks an example, run the rule over the example** — it is a
  one-line check, and the example is the part a reader will copy.

- **A comment, a test and a mutation all built on a consequence that this
  codebase cannot produce — database browse, Task 9, 2026-09-04.** The
  planning claim was that swallowing a 404 on the row viewer would "close the
  entire operator surface" — a password prompt where a missing table should
  be. It is false here, and the reviewer established it by tracing rather
  than reasoning: `useCloseSurfaceOnReauth` is the only thing on these
  screens that invalidates the flags query `AdminGate` watches, and it fires
  on `ADMIN_REAUTH_REQUIRED` alone; `adminFlagsKey` has exactly three
  writers, none on a read path; and `main.tsx`'s `QueryCache` has no
  `onError`. The real consequence of getting it wrong is a **blank page** —
  the heading, nothing under it, no error and no data — which is a worse
  screen but not a closed surface. The wrong belief had already produced a
  "why" comment shipped knowingly, a test asserting the absence of a prompt
  that could not have appeared (pattern 2 above), and a severity rating one
  level too high. All three were corrected. What would have caught it
  sooner: **before writing "and then X happens", find the code that would
  make X happen** — here that was three greps, and the answer was that
  nothing did.

- **The fix changed the sentence's subject and carried its predicate over
  unchanged — database browse, final review, 2026-09-04. Fifth instance of
  the same wrong clause, and the fourth fix is what produced it.** Four
  commits on this branch went into correcting the `«null»` example above:
  the comments on `domain.NullCell` and the screen's `Legend()` had used
  `users.password_hash`, a column that can only ever render `«redacted»`.
  The fix swapped the column to `users.email`, which is right, and left the
  clause after it exactly as it stood — so both files now read "`users.email`
  is NULL for a member who has only ever signed in with a magic link". A
  magic link is **sent to** an email address. A member who has one
  necessarily has an address; the account shape that leaves `email` NULL is
  Telegram-only. The subject moved and the predicate did not move with it,
  and the sentence went from true-about-the-wrong-column to false.
  As with the instance it grew out of, the branch's own test had the answer
  already: `browse_repo_test.go:122` says "email is NULL for a Telegram-only
  account (`UserRepo.Create` writes NULL when it is given no address)", which
  is correct, and it was correct in the same commit that made the comments
  wrong. What would have caught it sooner: **when you change what a sentence
  is about, re-read the rest of the sentence** — a clause written for the old
  subject is not automatically true of the new one, and a search-and-replace
  fix is exactly the shape that leaves it behind. See also pattern 1: this
  is a class fix that seeded the next instance of its own class.

**Treat a citation the way you'd treat a test assertion: something the next
reader can verify against the thing it names, not something to trust because
it reads confidently.** Nearly every instance above cost nothing to
produce and each would have cost under a minute to check — reading the
query, re-finding the line, opening the mockup, grepping for the clause a
comment claimed existed, reading the one function (`chi`'s `routeHTTP`)
whose order the claim depended on, reading the one helper (`convert.go`'s
`uuid`) a comment's causal claim depended on, or reading the one commit blob
a "before" claim was supposedly about — against a small planning gap, a
wrong number in a comment, a design claim nobody had opened the design to
test, a line range nobody had re-measured, a rule restated in a comment's
own words rather than pointed at, a guard whose comment named a failure mode
its own route could not reach, seven checkable claims that drifted the
moment the code moved under them on a single branch, a field silently empty
for its entire life because its own doc comment said otherwise, a sibling
comment one file away making the identical claim, already proven false, that
nobody had gone back to check, and a floor comment naming a task and a
number for a drift that had happened over several. That last one arrived
with company the catalogue cannot count, because it never landed: the fix
wave's own instructions for closing out this pattern handed down a further
wrong number for the same passage, and it was caught in review rather than
committed — the only reason it is a footnote here instead of an entry of its
own. **Two of them cost ten minutes reading a vendor's source instead of a
minute rereading this repository's own comment** — Mailpit's `link-check`,
and pgx's own error-wrapping — the same discipline, aimed at an API's
implied promise instead of a comment already in the tree. **They are not
equally good outcomes, and the difference is the whole point of this
pattern.** The `link-check` claim was checked while the design was being
written and never entered a file. The pgx claim was written into a comment,
committed, and only then read against the source — by the implementer, who
had first reported the opposite and went back to check, and then
independently by the reviewer. It cost a fix round. A wrong claim in a
commit has shipped, whatever a later commit does about it. A citation checked
once and never re-verified when the sentence around it is rewritten is not a
citation any more; it is the same unverified claim wearing a reference.
Neither is a name an endpoint gives itself.

**This paragraph used to open by counting its own list, and that number was
wrong within one branch of being written.** It now says "nearly every" and
names only the exceptions, deliberately: a closing summary that enumerates is
the identical defect the whole pattern is about, one level up. State the
invariant, not the enumeration — the same rule the admin surface's handover
distilled from nine of its own comments.

---

### 17. A requirement the plan drops is invisible to every review that reads the plan

The database browse's design spec has a paragraph in its Testing section
headed, in as many words, **"The test that must exist and would be easy to
omit"**: a schema-driven redaction sweep that reads every column of every
table of a migrated container and asserts that each one whose type is `bytea`,
or whose name matches the name rule, comes back `Redacted: true`. The spec
even says why it must be schema-driven rather than a fixture list — so that a
migration adding `webhook_secret bytea` next year is covered by a test written
today.

It was never written. Grepping the 3349-line plan built from that spec for the
paragraph's wording returns nothing: **the plan never carried the requirement
forward.** From there the outcome was determined. Eleven implementers worked
from task briefs cut from the plan; eleven task reviewers checked each task
against the plan; a fix round followed every review that asked for one, and a
fifteen-criterion browser walk ran on top of all of it. Every one of those
checks was looking at an artifact the requirement was not in, so all of them
were blind in exactly the same place — not eleven independent chances to catch
it, one chance repeated eleven times, and each fix round scoped to the finding
that produced it. It surfaced only in the final whole-branch review, which
read the spec.

**The omission was load-bearing twice, and that is the part worth keeping.**
The test that was dropped is the one thing that would have caught the same
branch's largest finding: `information_schema.data_type` does not carry the
type name for arrays or domains (`bytea[]` reports `ARRAY`, a domain over
`bytea` reports `USER-DEFINED`), so the redaction rule's "type first" promise
was false for the two likeliest shapes a future secret takes. The check and
the thing it checks went missing in the same act, which is why nothing
downstream noticed either.

**Two traps found while finally writing it, both of which would have produced
a green test that proved nothing:**

- *Written to the spec's literal words, it would have certified the blind
  spot.* "Reads every column … from a migrated container's
  `information_schema`" invites an oracle built on `data_type` — the same
  column the code was wrong about. Oracle and code would have agreed forever
  while `bytea[]` went unredacted, and the branch would have shipped with a
  test whose name claimed the opposite. The oracle has to be independent of
  the thing under test: here that meant walking `pg_type` through `typelem`
  and `typbasetype` and asking whether `pg_catalog.bytea` is anywhere in the
  chain — which also resolves domains, so the test covers a case the
  predicate deliberately does not.
- *The real schema alone could not carry it.* All five `bytea` columns in
  this database are also named `*_hash`, so the name rule covers every one of
  them: a sweep over the migrated tables passes with the type rule **deleted
  outright**. The test creates a table of its own holding `bytea` and
  `bytea[]` under names no name rule matches, and only then does deleting the
  type rule turn it red. A schema-driven test is only as strong as the
  schema's own variety, and a schema can be uniform by accident.

What would have caught it sooner, and it is a thirty-second check:
**before execution starts, diff the spec against the plan.** Every test the
spec names — by the words the spec names it with — has to appear somewhere in
the plan's task list, and a `grep` per named test is the whole procedure. The
spec is the binding authority (`CLAUDE.md` says so); the plan is a derived
artifact, and derivation loses things silently.

The second half is about reviews rather than plans. **A review scoped to a
task can only ever check that task.** Something the plan never contained
cannot be found by any number of task reviews, however careful each one is —
only by a review that reads the source document. "Eleven task reviews passed"
and "the spec is satisfied" are different claims, and this branch is the
evidence that the first does not imply the second. That is the job a final
whole-branch review against the spec exists to do, and it is worth budgeting
for on every plan rather than treating as a formality at the end.

Pattern 13 is this one's sibling from the other direction: there, the walk
faithfully executed a spec that was itself wrong. Here the spec was right and
the derivation dropped it. Both say the same thing about derived artifacts —
**check the derivation against its source, not only the work against the
derivation.**

---

## Catalogue by area

### Domain and money

- `Money.Add` wrapped silently on overflow. Every balance flows through it.
- `Money.String()` rendered `math.MinInt64` as `-SGD -92233720368547758.-8` —
  negating the most negative int64 returns itself.
- `Money{}` zero values added successfully, because the currency guard compared
  `""` to `""`.
- The last-owner rule never checked the target existed, so removing a
  non-existent membership was approved — and a capability-only edit on a limited
  member tripped `ErrLastOwner` for an operation that never touched ownership.
- `Rate.Apply` multiplied without an overflow guard while `Add` refused to wrap.
  Multiplication overflows far sooner.
- A doc comment promised "an invalid value cannot exist anywhere in the system".
  Go cannot enforce that with exported fields, and the repository layer rebuilds
  these values from database rows.
- `switch` statements on `Role` and `Visibility` had no `default`, so an
  unrecognised value — which arrives from a text column — skipped validation
  entirely. **Fail closed on values you did not construct.**
- `NetWorthSummary.Computable`'s guard was `len(views) > 0 && converted == 0`,
  which counts archived accounts — the loop skips them outright before either
  counter increments, so a household whose only accounts were archived would
  have reported "we cannot compute your net worth" for what this same feature's
  own rule calls a genuine and computable zero. Caught by the implementer
  before it shipped, not by a test; fixed with a `considered` counter
  incremented only for non-archived views, judged separately from the raw row
  count.
- An account's balance excluded every transaction dated on its own
  `opening_balance_as_of` day (`account_repo.go`'s two sum sites compared
  `occurred_on > opening_balance_as_of`), matching Transactions spec
  decision 6's "strictly after" rule exactly — the implementation was
  correct against the spec and the spec was wrong (pattern 13). Fixed to
  `occurred_on >= opening_balance_as_of`, with the two before-flags
  (`beforeFromAccountOpeningBalance`/`beforeToAccountOpeningBalance`)
  flipped to strictly-before (`<`) to match the new boundary. The sibling
  hunt for this one change found five more prose statements of the old rule
  outside the six SQL comparisons the plan named: `AccountView.Balance`'s
  doc comment in `usecase/ports.go`, `account_repo_test.go`'s comment
  referencing it, `domain.Account`'s `OpeningBalanceAsOf` doc comment,
  `accountDTO`'s doc comment, the `00004_accounts.sql` migration's table
  comment, and the frontend's mirrored comment in `schemas.ts`.
- `domain.PercentUsed`'s rounding (add half the denominator before
  truncating, for half-away-from-zero) only rounds correctly for a
  non-negative numerator. A household with refunds exceeding the month's
  spend has a negative net Spent, and adding a positive half-denominator to
  a negative numerator rounds it *toward* zero instead of away from it —
  `-338000` spent against a `520000` budget is exactly -65%, and the
  unsigned formula returned -64. Fixed by making the rounding term
  sign-aware (subtract half for a negative numerator, add for
  non-negative), with a test for the exact -65% case and a second one at a
  boundary the original formula's own test suite had never actually landed
  on (127400/520000 = 24.5%, rounding up to 25 — "half rounds up" was
  already asserted, but with a numerator that happened to give exactly
  25.00%, never exercising the rounding term at all).
- `web/src/features/money/budgetTemplates.ts`'s 50/30/20 split computed each
  pool as `incomeMinor * 0.5` / `* 0.3` before flooring it into whole minor
  units — `float64` arithmetic in a monetary path, the exact thing
  `CLAUDE.md`'s money rule exists to keep out, and not hypothetical:
  `333333 * 0.3 === 99999.90000000001` in JavaScript, so a pool that should
  floor to exactly 99999 could floor one unit low depending which side of
  the float error a given income landed on. Fixed to integer-first
  division-then-multiplication, never a fractional float literal, with a
  regression test pinned to the exact income that exposed the drift.
- Budget slice, final review: `BudgetService.Save` refused a negative
  `capMinor` (`domain.ErrBudgetCapNegative`, service check plus the
  migration's `CHECK` on `cap_minor`) but had no equivalent guard for its
  sibling field `expectedIncomeMinor` — neither a service check nor a
  database constraint. `domain.NewMoney` does not itself reject a negative
  amount (a transaction's `Money` can legitimately be negative), so a
  direct owner+CSRF `PUT /budgets/{month}` with
  `{"expectedIncomeMinor": -500000, "lines": []}` stored the value and
  round-tripped on the next `GET`, unnoticed because every existing test
  exercised the cap field's guard and never the income field's. Fixed with
  the same per-field-sentinel shape as the cap check
  (`domain.ErrBudgetIncomeNegative`, checked in `Save` before any repo
  call, mapped to 422 `NEGATIVE_BUDGET_INCOME`). The lesson isn't the bug
  itself but why it survived review as long as it did: two fields of the
  same type (`int64` minor units, same `NewMoney` constructor, same
  "can this be negative" question) got asymmetric treatment, and nothing
  short of asking "what's the sibling field's guard, and does this one
  have the same shape" would have caught it — see pattern 1.
- Goals slice, final whole-branch review: `BudgetRolloverCard.tsx`'s "done"
  sentence ("S$650.00 moved into Bali trip.") rendered `remainingMinor` —
  `BudgetMonthView.Remaining`, `Budgeted − Spent`, **recomputed on every
  `GET /budgets/{month}`** — as if it were a record of the amount a past
  rollover had actually moved. Nothing in this codebase blocks a backdated
  transaction, an edit, or a delete inside an already-rolled-over month
  (`TransactionService.Create` has no closed-month guard; neither does
  `BudgetService.Save`), so any of the three silently changed a past-tense
  sentence's own number after the fact — a late July receipt entered in
  August could turn "S$650.00 moved into Bali trip" into "S$500.00 moved
  into Bali trip" on the very next page load, while the goal's own
  contributions panel still showed S$650.00 for that same row. The general
  shape: **a live recomputation rendered as though it were a frozen record
  of a completed action.** `remainingMinor` was never wrong — it was
  answering a different question ("what is unspent right now") than the
  sentence asked ("what did this rollover move"), and nothing before this
  review had asked whether those two questions could drift apart once time
  (and more transactions) passed between them. Fixed by adding
  `rolloverAmountMinor` end to end — `BudgetRepository.Get`'s own query
  `LEFT JOIN`s `goal_contributions` (`household_id` + `source_budget_month`
  + `source = 'budget_rollover'`, at most one row by the partial unique
  index `goal_contributions_one_rollover_per_month`) rather than a second
  column on `budgets`, through `domain.Budget` → `BudgetMonthView` → the
  DTO → the zod schema → the component, which now gates its "done" branch
  on that field instead of `rolledOverTo`. The regression test
  (`TestBudgetMonthRolloverAmountSurvivesALaterTransaction`, plus an HTTP
  round-trip covering the same scenario against a real database) rolls a
  month over, adds a transaction to it afterward, and asserts the reported
  amount is unchanged while `remainingMinor` demonstrably moved — a test
  that only checked the amount right after the rollover would have passed
  against the original bug just as easily. Ask, for any figure rendered
  next to a past-tense sentence about a completed write: is this value
  actually read back from what was written, or is it recomputed fresh on
  every read of the surrounding screen?
- Bills slice, Task 8: `BudgetService.Month`'s `Spent` accumulator had no
  guard on `PaidByMembershipID`, but the `ByPerson` breakdown built two lines
  later did — `if t.PaidByMembershipID != ""` — so a transaction saved
  without a payer counted toward the total above the Spending-by-person card
  and was silently dropped from the rows under it. The card's rows could sum
  to less than the figure they sit under, with nothing on screen saying so;
  a hand-entered transaction was the only way to trigger it before Bills,
  but a bill with no "Paid by" makes it the common case once Bills ships.
  Fixed by removing the guard and accumulating on the possibly-empty key —
  the same shape every other accumulator in that loop already used — with
  the unattributed bucket (`MembershipID ""`) appended to `personOrder`
  after the loop so it sorts last regardless of when its first transaction
  appeared, rather than at its own first-appearance position. Copy for the
  row ("Unattributed", and the explanation beneath the card) lives in
  `budgetCopy.ts`/`BudgetByPerson.tsx`, never composed in Go — `Name`
  arrives `""` on the wire on purpose. **A total and a breakdown of that
  total must apply the exact same filter; the moment they diverge, the
  breakdown can quietly stop reconciling with the number above it, and nothing
  short of summing the rows and comparing catches that** — the same shape as
  pattern 5's silent partial success, on a read path instead of a write.

### Database and repositories

- `MarkInviteAccepted` had no guard and no `RETURNING`, so two concurrent
  accepts both succeeded. The correct pattern was already forty lines above in
  the same file (`ConsumeMagicLink`).
- A unique-violation surfaced as an opaque driver error because `translate` only
  special-cased `pgx.ErrNoRows`.
- Budget slice, Task 5: four of `BudgetRepo.Upsert`'s own statements
  (`DeleteBudgetLines`, `InsertBudgetLine`, `GetHouseholdPrimaryCurrency`,
  `CountCategoriesInHousehold`) were wrapped with a plain `fmt.Errorf`
  instead of `translate()`, the same "raw driver error crosses the adapter
  boundary" shape as the bullet above, just at four new call sites instead
  of the original one. Reachable for real: two lines in one `PUT` payload
  naming the same category id both pass `validateLineCategories` (it
  dedupes before counting), then the second `InsertBudgetLine` hits
  `budget_lines`' own `UNIQUE (budget_id, category_id)` after
  `DeleteBudgetLines` has already run inside the same transaction — a
  `*pgconn.PgError` would have reached `usecase.BudgetService`, not
  `domain.ErrAlreadyExists`. The existing test
  (`TestBudgetUpsertIsOneTransaction`) couldn't have caught this: its own
  failure happens before any write starts, so nothing in it ever pinned
  `pgx.BeginFunc`'s rollback once writes were already underway. Fixed by
  routing all four through `translate()` and adding
  `TestBudgetUpsertDuplicateCategoryLineRollsBackAndStaysAtTheBoundary`,
  which proves both the rollback undoes the `DELETE` and that the error
  surfacing from it is `domain.ErrAlreadyExists` with no `*pgconn.PgError`
  reachable via `errors.As`.
- Budget slice, Task 6: `CategoryRepo.Create`'s own comment claimed its
  single-statement `MAX(sort_order)+1` insert closed the concurrent-create
  race entirely — a comment stating something the code does not actually
  do, the same shape pattern 1's `buildRows` entry warns about. It closes
  only the round-trip window (this process's own read then write); under
  `READ COMMITTED`, two overlapping transactions can each read the same
  pre-insert max and both commit the same `sort_order`, because
  `sort_order` carries no unique constraint. Found by re-reading the claim
  against the isolation level actually in use, not by a test — a tied
  `sort_order` is cosmetic (display order, not correctness) and was judged
  not worth an advisory lock or a unique constraint, so the fix corrected
  the comment to name the residual race and the accepted trade-off rather
  than closing it, and added `, name` as a tie-break to both `List` queries'
  `ORDER BY` so a tie's *display* order stops depending on whichever row
  order Postgres happens to scan on a given read.
- `ListSpaces` ordered by `position` with no tiebreaker, so duplicates made
  sidebar order nondeterministic.
- Deleting a membership leaves the `users` row alive. Three separate symptoms
  traced to that one fact, and no task owned "what if this email already
  exists".
- Widening a sqlc-generated params struct (`CreateHouseholdParams` gained two
  currency fields) does not fail the build at a call site that omits the new
  fields. sqlc generates a keyed struct literal, so Go silently zero-values
  whatever a caller leaves out; `go build` and `go vet` both stay green — a
  plan that predicted a compile error here was wrong. The existing round-trip
  test asserting the persisted values, not the compiler, is what would have
  caught a dropped field. The same keyed-literal blind spot applies to any Go
  struct, not just sqlc's, which is why a later task re-checked all 17 call
  sites of the higher-level `HouseholdRepository.Create` by hand for the
  identical reason.
- The instinct on `transactions.from_account_id`/`to_account_id` was `ON
  DELETE RESTRICT` — the application never deletes an account, only archives
  it, so a restrict looked free. It would have broken deleting a *household*:
  the household cascade reaches its own accounts, and a restrict from
  transactions would block that cascade partway through, with no way to
  remove a household that had ever recorded a transaction. `ON DELETE
  CASCADE` is correct precisely because the clause is unreachable in ordinary
  use and must not stand in the way the one time it fires. Found by reasoning
  about the cascade before the schema shipped, not by a test — a test that
  deletes a household with transactions still in it now exists
  (`TestDeletingAMemberKeepsTheirTransactionsAndDeletingAHouseholdTakesThemAway`;
  see pattern 2 above for what that same test's own comment got wrong about
  what it proves).
- `TransactionRepo.List` builds its optional filters as the standard "`IS
  NULL OR column = $n`" form — no filter means match everything. But
  `uuid()`'s own doc comment promises that a malformed id "matches no row,"
  and parsing a garbage string produces `pgtype.UUID{Valid: false}`, which
  reads as SQL `NULL` — indistinguishable, in that `IS NULL OR ...` form, from
  an *absent* filter. A garbage `accountId` therefore silently became "no
  account filter" and returned the whole household's ledger: the opposite of
  what the same helper's own comment promises, and the opposite of `Kind`, a
  plain string that fails closed on the same kind of bad input (an
  unrecognised value can never equal any row's `kind` column, so it correctly
  returns nothing). Fixed by checking `id.Valid` explicitly for every
  optional uuid filter (`AccountID`, `CategoryID`, `PaidByMembershipID`) and
  returning an empty result when it is false, rather than falling through to
  a query that cannot tell "invalid" from "unset." The repository guard is
  not the only line of defence: the handler itself already refuses a
  malformed `account_id`/`category_id`/`paid_by` at `422` before the
  repository is ever called, and the paging cursor's id half is checked
  separately again in `decodeCursor`, deliberately, since `TransactionRepository.List`
  has no `Valid` guard on `CursorID` the way it does on the other three.
- Bills slice, Task 5: a prescribed mutation check on `BillRepo.RecordPayment`
  — remove `defer tx.Rollback(ctx)`, commit after each write, confirm the
  test the brief named goes red on "a payment row survives a failed write" —
  went red for a completely different reason. `TestRecordPaymentIsAtomic`'s
  bad-category input fails on the transaction's own first write
  (`CreateTransaction`), so nothing existed yet for a partial write to
  leave behind; the actual failure was `panic: test timed out after 40s`,
  `t.Cleanup(db.Close)` hung inside `pgxpool.Pool.Close()` forever, waiting
  on a `sync.WaitGroup` no goroutine would ever signal. **An un-rolled-back
  pgx transaction does not just fail to undo its writes — it permanently
  leaks its connection back to the pool**, because pgx has no finalizer for
  an abandoned `pgx.Tx`; the pool's own `Close()` waits for every checked-out
  connection to be returned, and one that was never rolled back nor
  committed is checked out forever. In production this pool is long-lived
  and never closed mid-request, so the hang is a test-harness-only symptom —
  but it turns a mutation check that should read as a clean, fast assertion
  failure into a 40-second CI timeout with a `puddle.Pool.Close` stack trace
  that says nothing about the actual property under test. The mutation was
  still genuine (Step 6's letter was satisfied), but it proved "the deferred
  rollback is what lets the test suite finish at all," not "a partial write
  is visible" — a different property than the one it was written to catch.
  The implementer added `TestRecordPaymentLeavesNoOrphanExpenseWhenTheSecondWriteFails`,
  which pays the same occurrence twice so the second call's first write
  (the expense) really does land before its second write is rejected by
  `UNIQUE (bill_id, due_on)` — the only shape where a write has something to
  leak before the failure — and re-ran the same style of mutation against
  it, watching it go red on the actual claim (`got 2 transactions, want 1`).
  **If a repository test hangs instead of failing an assertion, check
  whether the transaction under test ever got something to roll back before
  its forced failure fired** — a hang there is a leaked connection, not a
  flake to retry.
- Marriage/Retros, Task 7: **a third gate nobody had named, found only
  because a mutation test needed to build state that turned out to be
  unbuildable.** The guard's mutation test wanted to prove
  `requireCapability(marriage)` stacked on `requireOwner` really does refuse
  a limited member holding marriage — the brief's own instruction was to
  build that state at the repository level, matching how every earlier
  money-group guard test in this codebase constructs its fixture. It cannot
  be built that way here: `membership_repo.go` carries a database `CHECK`
  constraint, `limited_members_have_no_marriage`, that refuses the insert
  outright, deliberately, and undocumented anywhere the plan's own author had
  read before writing the brief. The state the test needed to construct — a
  limited member who somehow holds `marriage` — is one the schema itself
  makes impossible to create, which is stronger than any guard code above it
  could be, and stronger than the plan expected. Fixed by building the
  fixture with a `MembershipRepository` double instead of a real insert, the
  only way to represent a state the database refuses to store; the two
  application-layer guards (`requireCapability`, `requireOwner`) are still
  proven independently against it. **A defence can be real and still
  invisible to the plan that assumes it isn't there** — the CHECK constraint
  was doing its job the entire time, silently, and the only reason it
  surfaced at all was a test that needed to defeat it and could not.
- Vision spec, Task 1: **a referential action is a write, so every
  write-time constraint applies to it — including one that only ever meant
  to police `INSERT`.** `vision_measures` began, in spec review, as a
  two-branch `CHECK` (a
  measure is either typed or linked to a goal, never both, never neither).
  `goal_id` is `ON DELETE SET NULL`, so deleting a goal executes an `UPDATE`
  on every measure that pointed at it — and Postgres enforces `CHECK`
  constraints on `UPDATE` exactly as it does on `INSERT`, not only the
  statement a reader would picture first. The `SET NULL` sets `goal_id`
  alone, leaving `target_value`/`current_value` still `NULL` from the
  measure's linked state — a row the two-branch CHECK refuses, so **deleting
  a savings goal would have raised a constraint violation from inside the
  Goals feature**, the one place nobody debugging a failed goal deletion
  would think to go looking for Vision. Caught reasoning through the cascade
  before any code existed, the same way the `transactions.household_id`
  `CASCADE`-vs-`RESTRICT` entry above this one was — not by a test, because
  no test yet existed to fail. The schema now carries a third, explicit
  branch for exactly this all-`NULL` shape, and
  `TestDeletingALinkedGoalUnlinksTheMeasureInsteadOfFailing` proves deleting
  the goal succeeds and leaves the measure's row present rather than
  erroring. **Whenever a foreign key on a CHECK-constrained table carries
  `ON DELETE SET NULL` (or `CASCADE`'s own `UPDATE`-shaped cousins), ask
  what the constraint says about the row the referential action itself is
  about to produce** — not only about the rows the application inserts.
- Vision spec, Task 5: **a repository method that opens a transaction must
  never call a method that acquires its own connection — even a read, even
  its own `Get`.** `VisionRepo.Save`'s stale-`UPDATE` path needed a second read to tell "the
  row is gone" apart from "someone else saved first" (`RetroRepository`'s
  own pattern), and the first version of it called the pool-backed `r.Get`
  — four separate queries, each acquiring a connection from `pgxpool.Pool`
  — from inside the `pgx.BeginFunc` transaction that was already holding one
  connection checked out. One such call is merely wasteful, borrowing a
  second connection it did not need to. Enough of them landing at once,
  against the pool's own `MaxConns`, is not a slowdown: every concurrent
  version-guarded save blocks on a connection that only a save ahead of it
  in the same queue could release, and none of them can, because every one
  is holding its own first connection open waiting for a second. Found in
  review, reasoning about what happens under concurrent saves — no test
  in this codebase drives real concurrent database load, so nothing here
  would have gone red on its own. Fixed by moving the existence check onto
  `q`, the transaction-scoped `*sqlcgen.Queries` the open transaction
  already holds (`ab2e5cf`), the same discipline `RetroRepository` gets for
  free by never re-reading from inside its own transaction at all. **Inside
  an open transaction, every further read has to run on that transaction's
  own connection — reaching back out to the pool from in there is a request
  for a second connection while the first is still yours.**
- **Admin-surface branch, a near-miss rather than a shipped defect — reusing
  a lockout ledger silently changes whose lockout it is.** The plan's own
  sketch for admin re-authentication would have recorded a wrong password
  into `login_attempts`, the same table `AuthService.SignIn` already writes
  to. That table's own lockout is *household*-scoped:
  `domain.DefaultLockoutPolicy` locks password sign-in for every member of a
  household after three failures in fifteen minutes, on the reasoning that a
  household shares one front door. Feeding an operator's admin re-auth
  mistypes into the same ledger would have quietly widened that door's
  blast radius from "one household, on a screen every member can see" to
  "one household, over a mistake made on a screen nobody else in it can even
  see" — an operator fumbling their own password on `/admin` would lock
  their family out of the ordinary product as a side effect. No code ever
  shipped this; it was caught by reading `login_attempts`' own scope before
  reusing the table, not by a test — there is nothing to assert against
  until the wrong choice has already been made. The fix taken was a second
  table, `admin_reauth_attempts`, keyed on `user_id` rather than
  `household_id`, evaluated by the identical `domain.LockoutPolicy` so the
  *policy* is shared even though the *ledger* is not (see [ADR
  5](adr/0005-platform-admin-authorization.md)). None of the sixteen named
  patterns above fits this shape exactly — the closest is pattern 11's
  reasoning about asymmetric blast radius between a limit that inconveniences
  one caller and one that fails wider than intended, but that pattern is
  about two limits composing badly, not one piece of shared infrastructure
  carrying a scope built for a different audience than the one about to reuse
  it. **Before writing a failure, an attempt, or a lockout into a table
  someone else already owns, read what that table's own scope was built to
  contain** — a `household_id` column is a decision about who a lock can
  reach, and a new caller inherits that decision whether or not it was
  written for them.
- **sqlc types a column selected through a `LEFT JOIN` onto a *derived table*
  as non-nullable, and the scan then fails at runtime — admin households,
  2026-09-02.** `SearchHouseholds` names the member whose display name or
  email matched the operator's search, through a `LEFT JOIN LATERAL (SELECT
  u.display_name, u.email FROM ...) mm`. `users.display_name` is `NOT NULL` in
  the catalogue but genuinely NULL in *this* result whenever the lateral finds
  no match — every row of an empty search, and every household that matched by
  its own name. sqlc generated `MatchName string`, and pgx refused the scan
  outright: `cannot scan NULL into *string`. Not a mistyped value, a crash, on
  essentially every real request. Casts, `CASE`, `NULLIF` and an explicit
  column list were all tried and none changed the generated type; it is
  sqlc-dev/sqlc#3667, unresolved. The working pattern was already in this
  repository: `GetTransaction` selects `u.display_name AS paid_by_name`
  through a **plain** `LEFT JOIN users u` — a real table, not a subquery — and
  sqlc gets it right. The query was restructured so the lateral yields only
  `m.user_id` (never an output column, so its own type is irrelevant) with a
  second plain `LEFT JOIN users` supplying the two text columns. **A
  generated type that disagrees with the SQL is a runtime failure waiting for
  the first row that exercises it — check the generated struct's nullability
  against what the join can actually produce, not against the column's
  catalogue definition.**
- **A test fixture whose data contained the search term it was testing
  around — same branch, and the reason a correct implementation looked
  wrong.** The plan's fixture created the household "Andreas & Christine" and
  then asserted that searching `christ` finds it *through its member*
  Christine, naming that member in `Match`. But the household's own name
  contains `christ`, and the port's documented contract is that `Match` is nil
  when the household itself matched — so the test demanded the opposite of
  what `ports.go` says. It failed against correct code with
  `SearchHouseholds("christ"): Match = <nil>, want member "Christine"`. Fixed
  by renaming the fixture household to "Andreas & Kris", one token, with a
  comment saying why. **A fixture that accidentally satisfies more than one
  branch of the thing under test cannot tell those branches apart** — when a
  search test is red, check the haystack before the needle.
- **The SQL mutation checks on this branch, and what each one killed** (no
  defect found; recorded because a mutation nobody wrote down is a mutation
  the next person re-derives). Task 1, `TouchSession`: three mutations proved
  each of `last_seen_at`, `expires_at` and `admin_grant_expires_at` is
  independently pinned by the one-column-write test — see pattern 2 for the
  one that first went red for the wrong reason. Task 5,
  `CountActiveHouseholdsSince`: replacing `COALESCE(s.last_seen_at,
  s.created_at)` with `s.created_at` produced `ActiveHouseholds = 0, want 1
  (touched yesterday counts; signed in 10 days ago does not)` — the whole
  reason the column exists, pinned. Task 5, `SearchHouseholds`: deleting `OR
  u.email ILIKE ...` produced `SearchHouseholds("CHRISTINE@") = [], want
  exactly household 7dec6137-...` — the email half of the search predicate,
  pinned.
- **`LIMIT`/`OFFSET` without `ORDER BY` is not "unsorted", it is
  *unrepeatable*, and a small test table will never show you** (database
  browse, 2026-09-04). Postgres is free to return rows in any order, so page
  2 may repeat a row from page 1 and skip another, with no error anywhere.
  What makes it visible is not table size — a static heap read by one
  connection comes back in the same physical order at 5 rows and at 500, and
  the one thing that would shuffle it, a synchronized sequential scan, needs
  a table larger than `shared_buffers / 4`. What makes it visible is **a
  write between the two pages**: an `UPDATE` writes a new row version and
  allocates a new line pointer after the existing ones, so an unordered scan
  then returns `[a3, a4, a5, a1', a2']` and `offset=2 limit=2` yields
  `a5, a1'`. That is the shape a live operator screen actually meets, since
  the product is writing to these tables while somebody pages through them.
  `BrowseRepo.Rows` therefore orders by the primary key, falling back to
  `ctid` for a table that has none — arbitrary but stable within one read,
  and honest about being arbitrary. Pattern 2 carries the mutation-check
  story; this is the mechanism.

### HTTP layer

**This is the only place authorisation exists.** No service takes an actor. A
route with a missing guard has no second line of defence.

- `PATCH /household` and `PATCH /notification-preferences` shipped without
  `requireOwner` — a child could change household currency and every
  notification setting.
- `GET /household/members` returned every member's email to any authenticated
  member, including children.
- Nine `json.Decoder` calls with no size limit; three of them pre-auth.
- Two sentinels answered 500 for ordinary user input.
- The session cookie never slid — the database row was extended, the cookie's
  expiry was frozen at sign-in.
- `middleware.Recoverer` wrote a bare 500, the one response without the error
  envelope — and a panic is exactly when a quotable request id matters.
- There was **no structural test for CSRF or ownership**, behind a justification
  that confused introspecting the middleware chain with observing its behaviour.
  You do not need to compare function values; you need to send a request without
  a token and assert 403.
- `chi.middleware.RealIP` resolves the caller's address from the first
  non-empty of `True-Client-IP`, `X-Real-IP`, then `X-Forwarded-For` — with no
  configured trust list, so whichever of those the *client* sets outranks
  whatever the reverse proxy appends. The sign-up per-IP limiter added here is
  the first thing in this codebase that ever made `clientIP` a security
  decision, and it is exactly as strong as the edge's header rewriting, no
  stronger: one `curl -H "X-Real-IP: <vary>"` per request defeats it unless
  the proxy blanks the client-supplied headers first.
- `signUpRequestsPerIPPerHour` (10/hour) and `usecase.SignupGlobalDailyLimit`
  (200/day) each shipped correct and reviewed on their own, and did not
  compose: one IP, entirely inside its own hourly budget, could exhaust the
  global ceiling alone (10 × 24 = 240 > 200), after which sign-up silently
  mailed nothing platform-wide for up to a day. Now 5/hour and 1000/day,
  with `TestSignUpRateLimitsCompose` asserting the arithmetic against the
  live constants so the two cannot drift apart unnoticed again.
- The accounts redaction gate tested `Role == RoleLimited` — blacklisting the
  untrusted role instead of naming the trusted one. Identical behaviour
  today, since `owner` and `limited` are the only two roles, and silently
  wrong the day a third arrives (an adult who is not an owner, which this
  product will plausibly want): that role would receive every balance and
  the net worth, with no test going red, because the state is not yet
  representable to test against. Rewritten to test `Role != RoleOwner`
  instead. `Role` arrives from a database column that `convert.go` casts
  rather than parses, so only a CHECK constraint stands between an
  unrecognised value and this code — the same fail-closed rule as the
  `Role`/`Visibility` switch statements above, just written as a condition
  polarity instead of a missing `default`.
- The same redaction gate above fixed the *role* axis (whitelist `Role ==
  RoleOwner`) but left the *field* axis a blacklist: the handler built the
  full `accountDTO` and nilled `Balance`/`BalanceAsOf` by name. A new money-
  carrying field added to the struct later would reach every limited member
  with no test going red, because the existing test only checked that those
  two named fields were absent — true regardless of what else leaked
  alongside them. Fixed in the test, not the handler: it now asserts the
  redacted account's *exact* JSON key set, so adding a field forces a
  decision at the one moment it matters, instead of a rebuild-by-whitelist
  that risks drifting the other way.
- The accounts redaction's sibling, found but deliberately not fixed here
  (it belongs to a different feature): `member_handlers.go`'s
  `toMemberViewDTO` builds the full `userDTO` and blanks `Email` by name —
  the identical field-axis-blacklist shape as `redactedAccounts` above, just
  emptied rather than nilled. `TestMemberListWithholdsEmailsFromALimitedMember`
  (`api_test.go`) decodes the response into `memberListEntry`, a fixed struct
  with a fixed set of fields — `json.Decode` silently drops any key the
  struct doesn't declare, so a new field added to `userDTO` later would be
  invisible to every assertion in that test, the same way an unasserted key
  set was invisible to the accounts test before it was fixed. Latent, not
  live: nothing leaks today, because the email is blanked rather than
  omitted, so the wire shape is already stable either way. Left as a trap
  for the day someone adds a second personal field to `userDTO` and the test
  still goes green. Tracked as
  [issue #1](https://github.com/oandrz/Household/issues/1).
- **The session touch's throttle, pinned by mutation — admin households,
  2026-09-02** (no defect; the proof-of-test record). `requireSession` stamps
  `sessions.last_seen_at` only when it is null or at least an hour old.
  Replacing that condition with `if true` left three of the four tests green
  and produced exactly one failure, the discriminating one: `a request ten
  minutes after a touch moved last_seen_at from 08:26:27 to 08:36:27; the
  throttle is an hour`. A middleware that writes on every request is not a
  failure any status code would show, so the negative case is the only test
  that can hold the throttle in place.

### Frontend

- **A mutation must invalidate every cache its endpoint writes to, not every
  cache its own feature owns.** Bills' `useMarkPaid`/`useUndoPayment`
  invalidated only the bills keys — but `POST /bills/{id}/pay` writes a real
  `transactions` row and the undo deletes one. Mark a bill paid, click through
  to Transactions or Finances, and the expense was absent and balances
  un-moved for up to the 30-second `staleTime`. `useTransactions.ts` states the
  rule verbatim for its own writes, and the Bills hook was written by looking
  at `useAccounts.ts`'s *shape* rather than at what its endpoint touches.
  **The question to ask is "what rows does this request change?", never "which
  screen am I on?"** — and the honest scope matters both ways: `["budget"]`
  was deliberately left out, because Budget staleness after a money write is a
  pre-existing gap shared with hand-entered transactions, and fixing it on one
  path only would leave two write paths disagreeing about what a money write
  refreshes.
- **A primary action that opens a form the household cannot complete is a dead
  end, and it is usually first-run.** `BillsPage`'s "+ Add bill" opened a modal
  whose Pay from `<select required>` was empty when the household had no
  accounts — browser constraint validation on submit, no explanation, four
  clicks in. Both sibling screens already refused exactly this
  (`TransactionsPage` disables its button with a hint; `QuickAddMenu` gates
  `canAddBill` on `accounts.length > 0`), and one of them was gating *this very
  modal*. The prerequisite lives in the schema: `pay_from_account_id` is NOT
  NULL. **When a form's required field comes from another feature's list,
  the screen that opens it owns the empty case.** The fix has its own trap:
  `accounts.data?.accounts.length ?? 0 === 0` reads a still-loading query as
  "zero accounts" and disables the button on first paint for everyone — only a
  query that has answered may say a household has none.
- **Loading and failed are two states.** `if (!accounts.data)` in `BillModal`
  showed "Loading…" forever on a failed `GET /accounts`: react-query stops
  retrying, so nothing ever arrived to replace it. `BudgetModal` had closed
  the identical shape one feature earlier, comment and all. A `!data` gate is
  a bug wherever the query can fail; branch on `isError` first.
- **A live clock read locally is not the month the server scoped.** Bills'
  "All caught up — everything due in September is paid" derived `allCaughtUp`
  from server figures scoped to the **UTC** month, and took the month *name*
  from `new Date().toLocaleDateString(...)`, which reads the **browser's**
  month. In SGT they disagree for the first eight hours of every month: at
  local 1 Sep 03:00 it is 31 Aug in UTC, so every August bill being paid fired
  the panel, which then named September while September's unpaid bills sat in
  Due soon beside it. The comment above the function argued there was nothing
  to guard against — correctly, about *parsing a stored date string*, which is
  what the sibling helpers do — and stopped there. **A comment that rules out
  one hazard reads as ruling out the family.** State what the label is
  describing, not only where its input came from.
- **The only page every member reaches is where "who can see what" stops being
  the router's problem.** Every other screen in this app sits behind
  `RequireAuth` plus `RequireCapability`, so "what does a limited member see
  here?" is answered before the component mounts — it doesn't. Overview is the
  first page with no such guard, and its three access shapes (owner, limited
  with `money`, limited without) are therefore *normal renders*, not edge
  cases. That inversion is easy to miss because the seeded owner account
  exercises exactly one of the three, so a walk that only signs in as the
  owner sees a third of the page's behaviour and calls it done. Two guards
  differing by one word made it worse: `/accounts` is
  `requireCapability(money)`, `/budgets/{month}` is `requireCapability(money)`
  **and** `requireOwner`, so a card built on the assumption that the two match
  403s for every limited member. **When a page has no guard above it, write
  down its access shapes before writing the page, and walk every one of them.**
- **A component that mounts a modal unconditionally pays that modal's fetches
  on every visit.** `TransactionModal` calls `useCategories()` itself, so
  `{open && <TransactionModal open .../>}` and
  `<TransactionModal open={open} .../>` differ by one request per page load —
  and `TransactionsPage` already carried a comment saying exactly that. The
  interim Overview's plan reintroduced the eager form on the app's front door,
  the single most-visited route. Caught by copying the house pattern rather
  than the plan, and pinned by a test that fails if either modal is mounted
  eagerly. **`open` as a prop is not the same as mounting conditionally**;
  when a child owns queries, the difference is the whole cost.
- A failed magic-link request rendered **nothing at all** — and because the
  backend send is fire-and-forget by design, the frontend is the only place a
  failure can ever surface. It is also the only way back into a locked household.
- Sign-in discarded any error that was not an `ApiError`, so a network rejection
  showed an empty form.
- The locked sign-in form was a dead end: the submit button was disabled by an
  error that could only be cleared inside the submit handler.
- `"Forgot?"` had no pending guard while its neighbour did, and neither
  validated the email — clicking with an empty field posted `{"email":""}`.
- Business rules were re-implemented client-side (the four-capability list,
  twice), which also made a required 422 path impossible for the UI to produce
  and therefore untested.
- A stale error survived `SignUpScreen`'s sent-panel → form transition: a
  failed resend, then "Use a different address", left the resend failure
  showing under the email field. Sibling of a defect already fixed once in
  `SignInScreen` (there, keyed off `mode` rather than cleared inside one
  handler) — the fix did not carry to the newer screen because nothing grepped
  for the shape.
- Task 13's brief named "Health" as one of the 50/30/20 template's needs-set
  categories, then gave the proportional split's weight table
  (`FAMILY_OF_FOUR_CAPS`) as exactly the ten Family-of-four categories, which
  does not include Health — the same brief text says two things that cannot
  both be followed literally (a needs category with "the family-of-four
  weight" it does not have). Rather than guessing which clause to drop,
  `budgetTemplates.ts`'s `splitPool` treats an unlisted name as weight zero
  by construction (`FAMILY_OF_FOUR_CAPS[name] ?? 0`), so Health silently
  funds nothing from either pool and — since it was never going to get a
  line anyway — is not reported in `missing` either, which would have
  wrongly told a household with a live "Health" category to go create one.
  The resolution and the reasoning are both written at the point a future
  reader would go looking for them (`NEEDS`'s own comment in
  `budgetTemplates.ts`), not left implicit in the code's behaviour, so the
  next person to touch this table sees the contradiction was noticed and
  decided, not missed.
- A task brief's own test snippet asserted on TanStack Router's `path`, which
  strips the leading slash on a child route (`trimPathLeft`); `fullPath`
  reconstructs it and is what a router-walk test must read instead. Would have
  failed against a *correct* implementation — verified against the router's
  own source, not assumed from the property's name.
- Adding a capability-guard route with only a nested child and no index route
  (task 10's `marriageGuardRoute`, one child: `retros`) does not make the bare
  parent path 404 the way it did before the route existed. Before task 10,
  `/marriage` fell through to `rootRoute`'s own `notFoundComponent` with
  `RequireAuth` never running at all, because nothing in the tree matched it.
  Once `marriageGuardRoute` exists (path `"marriage"`, nested under
  `shellRoute`), TanStack Router matches it as a real route for the bare path
  too — `RequireAuth` and `RequireCapability` both run, an unauthenticated
  visitor is bounced to `/sign-in` same as any real route, but an
  authenticated one who clears the guard sees `AppShell`'s sidebar with a
  *blank content area*, not a 404 and not the child page. Found empirically
  (a probe test, not a docs read) after a pre-existing router test asserting
  "`/marriage` renders Page not found" started failing for the wrong reason —
  it looked like the redirect assertion was broken, but the actual cause was
  that `/marriage` had quietly become a real, matched route. `moneyGuardRoute`
  never shows this because it has an index child (`moneyIndexRoute`, path
  `"/"`) that gives the bare parent path a real page — a route with children
  but no index is the shape that produces the blank-shell case. Check for an
  index route (or accept the blank content area explicitly, as task 10 did
  for Marriage — see `docs/FEATURE_TRACKER.md` section 6) whenever a new
  guard route's first child is not itself the parent's own landing page.
- A task brief's own `formatMoney` code butted every currency symbol directly
  against the digits (`Rp85,400,000`), contradicting the same brief's own IDR
  test (`Rp 85,400,000`) — neither could have been right as given. Fixed with
  a rule keyed on whether the symbol ends in a Latin letter, checked against
  all 18 currencies the backend serves. The same brief's flat, all-optional
  `Summary` schema also could not be narrowed by TypeScript after
  `if (!summary.computable)`, which would have forced a non-null assertion at
  the exact spot the DTO exists to prevent one; replaced with a discriminated
  union keyed on `computable`.
- The accounts feature's own file list left `FinancesPage.tsx` off, and wiring
  only `AccountsPanel`'s "+ Add account" button — as the file list implied —
  would have left account creation completely unreachable for a household
  with zero accounts: `AccountsPanel` is not mounted at all in that state,
  `FirstRunPanel` renders instead. Would have failed the feature's own
  definition-of-done walk at its very first "add an account" step. Found by
  reading `FinancesPage.tsx`'s own branching, not by a test; `FirstRunPanel`
  got its own button wired to the same modal.
- `FirstRunPanel` had no "Show archived" toggle at all, so an owner who
  archived their household's only account had no way back to it: the list
  emptied, `FirstRunPanel` took over, and nothing on it could ask for the
  archived view — decision 8's restore guarantee broken for exactly the
  household most likely to trigger it. The 15/15 browser walk never caught
  it because the seeded household always kept several accounts, so the
  single-account case never came up. The fix has two halves and both are
  required: the toggle moved into `FirstRunPanel`, and the branch condition
  dropped its `&& !includeArchived` clause. Dropping only the clause without
  adding the toggle leaves the state unreachable in a different way; keeping
  the clause after adding the toggle reintroduces a second bug one level
  down — a truly-empty household that tries the new toggle anyway falls
  through to the three-card zero state (`S$0.00` beside a blank breakdown),
  because switching `includeArchived` on still returns zero rows when there
  is nothing archived either. Both halves are mutation-tested separately:
  reverting either one turns exactly one of the two new tests red.
- `AccountModal`'s balance-parse error reused one message
  ("Enter an amount, like 8240.55.") for two different failures. Switching
  Currency to a no-decimal one (IDR, VND) without touching Balance is a
  common edit path — the balance display doesn't change on a currency
  switch — and it produces exactly that message next to a field showing
  precisely the figure it's being told to enter. Failing closed was already
  right; the copy just needed to name the actual cause instead of restating
  the input back at the person looking at it.
- Editing a transaction through `TransactionModal` (Task 15) resends every
  field's *current* value on submit, including the ones that are legitimately
  empty — `categoryId: null` for "no category" chosen, `receivedAmountMinor:
  null` for a non-transfer or an optional field left blank. The PATCH route's
  own fields are all pointers (`*string`/`*int64`, `transaction_handlers.go`)
  where a `null` in the JSON body and the key being absent entirely decode to
  the identical Go `nil` — "leave this alone." Forwarding the modal's `null`
  straight into the request body would make clearing a category (or a
  transfer's stored received amount back out) silently do nothing: the old
  value survives the exact request that was supposed to remove it, invisible
  behind a form now showing the field blank. `categoryId`, `paidByMembershipId`,
  `fromAccountId` and `toAccountId` need `?? ""` before the request is built —
  the same empty-string sentinel the create route's own zero-value default
  already uses for "no category." `receivedAmountMinor` has no honest empty
  sentinel of its own (`0` is a real, if invalid, amount), which is exactly why
  the API gives it a separate `clearReceivedAmount` boolean instead
  (`TransactionUpdate.ClearReceivedAmount`, `usecase/transaction.go`) — derived
  in `TransactionsPage.tsx` from whether the transaction had a received amount
  *before* the edit and does not *after*, not from the new value in isolation,
  because "the new value is null" alone is true both when a transfer's fee was
  genuinely just cleared and every time a non-transfer's form submits at all.
- `TransactionModal`'s Amount-received field used one flag
  (`receivedAmountTouched`) to gate two independent rules: whether to clear
  the field on a genuine currency-pair change, and whether to mirror it to
  Amount sent for a same-currency transfer. Typing a same-currency transfer
  fee (dbs → ocbc, both SGD) set the flag; changing the destination
  afterwards to a different-currency account (bca, IDR) left the old figure
  sitting in the field, because the touched flag now suppressed the clear it
  was never meant to gate — the amount would have submitted under the *new*
  currency's minor units, silently misvalued. One flag answering two
  questions is what let a rule written for the second question block the
  first. Fixed by splitting it into two independent states: a
  `currencyPairKey`/`lastCurrencyPairKey` comparison that unconditionally
  clears Amount received the moment the actual currency pair changes, leaving
  `receivedAmountTouched` to govern only the same-currency mirroring it was
  named for.
- The Transactions ledger's Kind filter hid its real `<input type="radio">`s
  with `sr-only` and never gave the visible `<label>` pill a rule reacting to
  the hidden input's `:focus-visible` state, so Tab and arrow-key navigation
  moved real, `:focus-visible`-true focus with no visible indicator at all —
  caught only by the Task 19 browser walk, since `fireEvent.click` (every
  existing test) never presses a key. See pattern 3 for the fix's own
  near-miss: a single ring colour was invisible on the selected pill's dark
  background until the colour itself was made conditional on which
  background it sits against.
- The grouped sidebar's Money links carried both `text-ink` and
  `text-accent` at once on the active link, because `Link`'s `activeProps`
  merges its className onto the base rather than replacing it; Tailwind's
  cascade always resolved to `text-ink` regardless of which link was
  active, invisible to every test asserting only that the class token was
  present. See pattern 3.
- The Add-account modal's "Visible to kids" copy and the currency panel's
  secondary-currency copy each hardcoded the design mockup household's own
  specifics (child names; "Christine's Indonesian accounts") as if they
  were generic product copy, safe only while the seeded household was the
  only one that existed. See pattern 14.
- `router.test.tsx`'s "mounts the Budget page" test stubbed only
  `GET /api/v1/auth/me`, with its own comment explaining why: BudgetPage was
  still Task 11's static stub, calling nothing else. Task 12 wired
  `useBudget`/`useCurrencies` into that same component and the test broke on
  the first run — `stubFetchRoutes` throws on any unregistered request, so a
  router test that had been green was quietly asserting "the placeholder
  never fetches," not "the real page mounts." The comment said exactly why
  the gap existed and that it would need closing, which made the fix a
  five-minute one instead of a debugging session, but a test whose pass
  depends on a component staying a stub is a landmine for whoever un-stubs
  it — worth writing that comment on every throwaway-stub test, not only
  this one.
- Task 12's first `useBudget.ts` was a hand-rolled `useState`/`useEffect`
  fetch hook, with no precedent check against `useTransactions.ts`'s own
  month-parameterised `useQuery`, already established for the identical
  shape (a resource keyed by which month is being viewed). The deviation
  was not free: it cost real behaviour, not just style. Category writes
  had nothing invalidating `useTransactions`' own `categoriesQueryKey()`
  cache entry, so a category created from the Budget modal would not have
  shown up in the transaction modal's dropdown without a full reload; and
  nothing kept `["budget", month]` as a structured query key, so switching
  the page's month picker mid-fetch had no cache boundary stopping a
  slower, stale response for the *old* month from landing after a faster
  one for the *new* month already had. `useQuery`'s own key-based
  cancellation is what `useTransactions.ts` got for free and this hook did
  not. Found by review, not a failing test — both gaps are races or
  cross-component effects a synchronous render-and-assert test does not
  naturally reach. Fixed by rewriting onto `useQuery`/`useMutation`
  matching the existing pattern exactly, with category writes now
  invalidating both query keys and `save()` actually parsing `PUT`'s
  response with `putBudgetResponseSchema` instead of leaving it uncalled.
  Five of the rewritten hook's own mutation tests needed `waitFor` instead
  of a synchronous post-act assertion, because TanStack Query's
  `notifyManager` re-render notification lands one microtask after
  `mutateAsync` resolves — the same timing `FinancesPage.test.tsx` had
  already worked around, not a new discovery, just a new place the same
  fact had to be applied again.
- `useBudget.ts`'s `createCategory` discarded the created category entirely
  (`mutationFn` returned `void`) and relied on invalidate-then-refetch for
  every caller to pick the new row up later — fine for every caller that
  existed at the time, all of which just needed the list to eventually
  refresh. `BudgetModal.tsx` (Task 14) is a caller that needs the server's
  own id *immediately*: its save flow POSTs a queued create, then has to put
  that exact id into the next request's line set, and the pinned
  create-then-PUT ordering test would have forced an extra `GET
  /api/v1/categories` between the two to look the id back up by name --
  noise in a sequence the test asserts on exactly. The fix was additive, not
  a rewrite: parse the create response and return it
  (`categoryResponseSchema.parse(raw).category`), which every existing
  caller still ignores and the new one now needs. General shape: a mutation
  hook that only ever invalidates-and-refetches is quietly assuming no
  future caller needs this write's own result before its *next* write --
  worth returning the parsed response even when today's callers throw it
  away, since the alternative (a second read wedged into an otherwise
  sequential write chain) is both slower and changes what an ordering test
  is actually asserting.
- `BudgetModal.tsx` calls `useBudget(month)` itself rather than taking a
  bound `save`/`createCategory` as props, which makes `budget.data` -- and
  the currency it carries -- unavailable on the component's first render
  whenever the query cache isn't already warm (any standalone test, or a
  slow first load). Seeding the form's row/income state from `initial` and
  `currency` in a plain `useState(() => ...)` initializer at the top level
  would make that seeding depend on load order: correct in the app (the
  parent always already resolved the same query) but wrong the moment a
  test renders the modal on a cold cache. Split into an outer component that
  render-branches on `!budget.data` and an inner `BudgetModalForm` that only
  mounts once data exists -- its `useState` initializers then run exactly
  once, at the one moment they have real data, and a later background
  refetch (e.g. after `save` invalidates its own query) cannot silently
  reseed a household's in-progress edits, since re-rendering the *outer*
  component doesn't remount the *inner* one.
- `BudgetModal.tsx`'s row-building fallback for a category id the modal
  can't name (`buildRows`, for a line whose category isn't in the active
  `categories` prop -- reachable today via "Import last month" handing a
  previous month's lines through unchanged, per BudgetPage.tsx's own
  comment, when a capped category has since been archived) set `name` and
  `originalName` from *two different* fallback expressions:
  `found?.name ?? "Unknown category"` for one, `found?.name ?? ""` for the
  other. Save's rename check is `row.name.trim() !== row.originalName` --
  with those two fallbacks, an unresolved row is `"Unknown category" !==
  ""`, unconditionally true, so every save on it queued
  `PATCH /categories/{id} {name: "Unknown category"}`, silently renaming a
  real (possibly archived) category. A same-value comment right above the
  code ("Save would still submit its real id and cap unchanged") was
  literally false the moment a rename fired alongside it — the comment
  described the *intended* behaviour, not what the two-fallback code
  actually did, and nothing checked the two against each other. Caught by a
  pre-commit design review, not a test that already existed — the fix
  computes the fallback name once and uses it for both fields, so an
  unresolved row is structurally incapable of registering as renamed. A
  comment describing what a value is *supposed* to be is not evidence about
  what the code actually does — and "two expressions that are supposed to
  agree" (here, two fallbacks for the same missing-name case) is exactly the
  kind of pair a single shared variable removes the chance of drifting.
- Task 14 shipped `BudgetModal.tsx` and marked "Edit budget (modal)" ✅ in
  `FEATURE_TRACKER.md` with the prose "a household can create and edit a
  budget end to end now" — but the only things that ever opened the modal
  were the empty state's template cards and "Create your first budget".
  There was no button anywhere that opened it for a month that *already*
  had one; `BudgetPage.tsx`'s populated branch (`data.budget !== null`)
  rendered the four stat cards, the category grid and spending-by-person,
  and nothing else. A household could create a budget once and then never
  change it again through the UI. Every one of Task 14's 13 tests rendered
  `BudgetModal` directly with props, which proves the modal works once
  open — none of them opened it *from the page*, so nothing ever exercised
  the one path that was missing. `docs/FEATURE_TRACKER.md`'s own ✅ was true
  about the modal and false about the feature: "the modal exists" and "a
  household can reach it" are two different claims, and only the narrower
  one had a test behind it. Caught here only because Task 15's own work
  touched the same header row (adding History) and `BudgetModal.tsx`'s own
  header comment had left a forward note anticipating exactly this gap
  ("a future 'Edit budget' entry point (Task 15) for an *existing* budget
  would normalise the same way") — without that comment, a
  reasonably-scoped History-only task could easily have shipped without
  ever opening the file that would have shown the missing button. General
  pattern: when a task's own tests only mount a component standalone, "is
  this component correct" gets covered but "can anything actually reach
  it" does not — a component with no verified trigger from its real parent
  is not shipped, even if every prop-driven test is green. A page-level
  integration test that opens the feature the way a person actually would
  (click the real button, from the real page state) is the only thing that
  would have caught this at the time it was introduced.
- `BudgetHistoryModal.tsx`'s three summary cards (avg spend, avg saved,
  months under budget) needed an explicit decision at two boundaries the
  brief didn't spell out, both written down as comments *and* pinned by a
  test that fails without the guard (not just asserted in prose): a closed
  month that spent exactly its cap counts as *under* budget, matching
  `BudgetStatCards.tsx`'s own treatment of a zero `remainingMinor` as
  healthy, not over; and a closed month with every cap removed
  (`budgetedMinor === 0`) is excluded from all three figures entirely,
  rather than being treated as a 100%-over month or dragging every average
  toward zero the way including it at face value would. Both guards were
  mutation-tested (flipped, confirmed red, restored) precisely because a
  green test against the *intended* boundary proves nothing if the fixture
  never actually lands on that boundary — the first draft of "months under
  budget" test data had no exact-cap month at all, so a `<=` vs `<` bug
  would have passed silently.
- Task 17's browser walk found two more defects in `BudgetModal.tsx`, both
  rooted in the same fact: the modal has two category lists, and only one
  of its two consumers was reading the right one. **Defect A**: `buildRows`
  resolved every row's name off `categories` (`BudgetPage.tsx`'s
  active-only `useCategories()` prop), so a line whose category was
  archived since it was capped fell to the shared fallback name from the
  bug ledgered just above this one -- "Unknown category" -- even though
  the same file's `addCategoryByName` already fetched
  `GET /api/v1/categories?includeArchived=true` for its own restore-vs-create
  check. The page's category grid renders "Petrol (archived)" correctly
  because it reads `useBudget`'s own per-month `categories`, which always
  carries archived rows -- a different, correct source the modal simply
  never consulted. **Defect B**: the "+ Add a category -> New category..."
  duplicate guard (`rows.some((row) => row.name === name)`) already existed
  and worked, but refused with zero feedback -- no message, no row, and
  (confirmed against the network log) no request at all -- reading as
  nothing happened rather than as a rejection. Fixed together: the
  archived-inclusive fetch moved out of `BudgetModalForm` and up into the
  outer `BudgetModal`, gated alongside `useBudget`'s own `budget.data`
  (with a fallback to the active-only prop on a genuine fetch failure, so
  a broken archived-inclusive endpoint degrades instead of hanging the
  modal on "Loading..." forever) so `buildRows`'s `useState` initializer
  never runs before that data exists, and handed down as a new
  `allCategories` prop used by both `buildRows` and `addCategoryByName` --
  while `categories` stays exactly what the add-dropdown filters against,
  since that list must never re-offer an archived category to pick again.
  The duplicate guard now sets a dedicated inline error next to the add
  control (reusing `categoryNameTaken`'s copy shape) instead of returning
  silently, and `handleAddNewCategory` only clears the "New category..."
  input on an actual add. Neither defect could have been caught by the
  existing suite as written: every test's `categories` prop and its
  `includeArchived=true` stub happened to already agree with each other
  (a fixture gap the fix also had to correct, once `buildRows` started
  depending on the second list), and no test ever typed an already-used
  name into the add control and asserted on the *absence* of a network
  call. Both new tests do; the archived-name one is mutation-checked
  (reverting `buildRows`'s argument back to `categories` turns it red on
  the display-name assertion, not just the incidental no-rename one, which
  would have passed either way). See pattern 1.

- Goals slice, Task 16 (Overview's "Goals on track" card): the implementer's
  own beyond-brief empty state computed `hasAnyGoals` as
  `datedCount + noDateCount` — the two counts `GoalsSummary` builds for a
  different question ("how many goals need an on-track pill"), which by
  design exclude an achieved goal (`GoalService.List`'s own switch checks
  `status == GoalAchieved` first and puts it in neither count). A household
  that fully funded its only goal without archiving it saw "No goals yet" on
  Overview while `/money/goals` still showed the goal, achieved pill and
  all — a derived boolean reconstructed from summary figures that were never
  built to answer the question actually being asked. This is the same shape
  as `NetWorthSummary.Computable`'s guard counting archived accounts
  (Domain and money catalogue, above) — a count built for one purpose reused
  for a different one it silently gets wrong at the boundary — and the same
  class as Task 11's own self-caught defect one task earlier in this same
  feature, where "Show archived" was gated on `goals.length > 0` and so
  vanished exactly when a household most needed it (archiving its last live
  goal). Three instances across two features is what makes this a class
  rather than a one-off: **a value built to answer "how many of X have
  property Y" is not safe to reuse for "does X exist at all" — count the
  thing you actually need to know, not the nearest count that happens to be
  in scope.** Fixed by reading `goals.goals.length` instead — the same
  question `GoalsPage` itself answers the same way — with a new test that
  builds the exact achieved-and-unarchived, both-counts-zero state and goes
  red on the old formula (commits `f04ce11..acbf52d`).

- Bills slice, Task 15 (the Subscriptions panel): wiring `SubscriptionsCard`
  into `BillsPage` turned five previously-green `BillsPage.test.tsx` tests
  red with `TestingLibraryElementError: multiple elements found`, on assertions
  that had never needed scoping before — `await screen.findByText("Car
  insurance")` used purely to wait for the page to finish loading, not to
  check anything about that bill in particular. `billFixture`'s own default
  is `isSubscription: true`, so once the panel existed, every fixture bill's
  name legitimately appeared twice on the page: once in its list row, once in
  the new panel's own row for the same bill. Nothing was wrong with either
  render — the tests were implicitly asserting "this name is unique on the
  whole page," a property that held by accident until a second, correct
  panel started showing the same data. Fixed by scoping each assertion to the
  section it actually meant (`within(dueSoonSection)`, or waiting on a
  `data-testid` instead of a name that was never guaranteed unique). **A bare
  `screen.getByText`/`findByText` on a value that also appears in test data
  elsewhere on the page is an unstated uniqueness assumption; it only fails
  the day a second, entirely correct feature reuses that same value** — scope
  to the section under test, or wait on structure (a testid) rather than
  content that a sibling component might legitimately repeat.

- Bills slice, Task 15 (the Subscriptions panel), found in review rather
  than by a test: `BillModal`'s "Counts as a subscription" checkbox does
  not gate on cadence, so a household can tick it on a one-off bill — and
  the dev seed already has one (`Piano tuning`, `S$120.00`, one-off). But
  `domain.AnnualEquivalentMinor`'s own comment is categorical: a one-off
  "is not a recurring cost" and contributes to the rollup **never**, ticked
  or not. The panel's first cut rendered the row anyway — a row that would
  visibly never move either total above it — while `isSubscriptionHelp`'s
  own copy tells the household every ticked bill is "included in the
  household's subscription totals," a promise that exact row would break.
  This is a sharper case than the ordinary "figure the UI can't back up":
  a flag the UI *lets a household set* that a downstream computation
  *categorically ignores for one specific value of a different field*, with
  nothing in either the checkbox or the field stopping the combination.
  Fixed by adding the same cadence check to the panel's own filter
  (`SubscriptionsCard.tsx`), verified against the real seeded bill in a
  running browser, not only the fixture. **When a flag and a second field
  interact, check what every value of the second field does to the flag's
  own promise — not just the values a form's own defaults happen to
  produce.** The checkbox itself is unchanged and still does not warn a
  household ticking it on a one-off; that residual gap is written down at
  `SubscriptionsCard.tsx`'s own filter for whoever next reads `isSubscription`
  without this same cadence check.

- **`min-height` does nothing on a plain inline element, and a `<button>`'s
  own centering is a browser default, not a CSS property you can rely on
  elsewhere.** The mobile-responsive plan's 44px touch-target floor used
  `min-h-11` everywhere, and it worked immediately on every `<button>`,
  `<select>` and `<input>` — those are form controls, and Chrome centers
  their content vertically inside a taller box by default. The same class on
  a react-router `Link` (which renders a bare `<a>`, `display: inline` unless
  something else sets it) does two different wrong things depending on
  context: as a plain inline element, `min-height` is defined by the CSS box
  model to have **no effect at all**, so the box never grows; once the link
  is blockified by being a flex-container's direct child (as every
  `Sidebar.tsx` nav link is), `min-height` *does* start applying, but nothing
  centers the text inside the taller box, so it sits pinned to the top with
  dead space below. The fix needed one more utility than the button case:
  `inline-flex items-center` alongside `min-h-11`, matching the earlier
  Kind-pill `<label>` fix that had already found the identical gap for
  `<label>`, another plain-inline element. `inline-flex` rather than `flex`
  matters too — a `flex`-declared link that is *not* already a flex child
  (a back-link sitting in a plain `<div>`, say) stretches to its parent's
  full width, silently enlarging its own click target into the empty space
  beside the text; `inline-flex` keeps shrink-to-fit sizing everywhere it
  isn't already being stretched by a parent, and blockifies to the same
  `flex` result everywhere it is. **Two different lessons in one class name:
  check whether the element is one of the small set of form controls that
  self-center before assuming `min-h-*` alone is enough, and use `inline-`
  variants for anything that starts out `display: inline` so a fix in one
  layout context doesn't become a regression in another.**
- **The same icon-button formula that is safe in an isolated corner can break
  a row with no slack.** `Modal.tsx`'s close button and `BudgetModal.tsx`'s
  remove-row button had both already shipped `h-11 w-11 sm:h-7 sm:w-7` with
  no visible cost — each sits alone, with margin around it. Applying the
  identical formula to `BudgetPage.tsx`'s `‹`/`›` month-picker glyphs, which
  share a 375px-wide row with History and Edit budget and had no spare width
  to begin with, pushed "Edit budget" onto two lines the moment the glyphs
  grew from ~4px wide to 44px — not a height problem, a width one, and one
  the phone's own household data (a budget already existing, so both
  siblings render) was needed to see, since an earlier session with no
  budget never renders the row tight enough to notice. Caught by
  screenshotting the change, not by trusting that a formula proven in one
  spot travels to the next one unmodified. Fixed by taking only the height
  half of the formula (`h-11`, no `w-11`) for this one pair, leaving the
  established square for every icon button that has the room for it. **A
  reusable formula is reusable up to the point a sibling's layout has less
  slack than the site it was proven on — screenshot the applied change in
  its own tightest real state before assuming the pattern travelled clean.**
- **A summary row's only count is not automatically the count a second
  caller needs.** `GET /retros`' `actionCount` was built for
  `RetroHistoryList` (spec: "K counts all of that retro's actions, ticked or
  not"), and it is the only number `retro.sql`'s `ListRetros` computed.
  Overview's `NextRetroCard` (Task 15) needed a different question answered
  — "is there anything still outstanding" — and read `actionCount` anyway,
  because it was the only field on the wire: a retro whose three actions
  were all ticked still showed "3 actions" on the home page, permanently.
  Task 15's own report named the gap explicitly rather than hiding it, which
  is what let it get closed later instead of forgotten. The fix was not a
  frontend filter (the list response carries no per-action detail to filter)
  but a second correlated subquery next to the first —
  `(SELECT count(*) ... WHERE done_at IS NULL) AS open_action_count` — carried
  through `RetroSummary`, `retroSummaryDTO` and the zod schema as
  `openActionCount`, a sibling field rather than a replacement, since
  `RetroHistoryList`'s own total is still correct for what it shows.
  Mutation-checked by making the new subquery ignore `done_at`: the Postgres
  test (three actions, two ticked, asserting `actionCount == 3` and
  `openActionCount == 1`) went red for exactly that reason. The frontend test
  needed a fixture where the two numbers actively disagree — `actionCount:
  3, openActionCount: 0` — to catch a `> 0` guard that was still reading the
  total; a fixture where they happened to match would have passed against
  that exact bug. **When two call sites read the same summary field for two
  different questions, that is a sign the field is doing two jobs — add the
  second number at the layer that can compute it correctly, rather than
  reusing the first number and hoping the difference never matters.**
  **The same discipline was applied at three layers here and missed at a
  fourth on the first pass.** The Postgres test disagreed the two counts on
  purpose; the frontend test disagreed them on purpose; but the HTTP
  wire-shape test that proves the field actually reaches the wire — added in
  the same task, to catch exactly the kind of hand-transcribed-JSON gap
  Task 7 had already found for itself — seeded a single, never-ticked
  action, so `actionCount == openActionCount` at that one seam and a
  regression writing `OpenActionCount: s.ActionCount` (the total, under the
  open field's name) would have passed it. Review named the missing case
  precisely; the fix round added a dedicated test with two actions, one
  ticked, and proved the gap was real rather than theoretical by running the
  same regression against both — the new test went red, the old one, with
  its coincidental fixture, stayed green throughout. **A rule this codebase
  already knows ("make the fixture disagree") has to be re-applied at every
  layer a value crosses, not assumed to travel with the value** — a fixture
  chosen for one layer's convenience can accidentally satisfy a neighbouring
  layer's whole reason for existing.

- **A hover state that replaces a status tint erases the status.**
  `RetroHistoryList`'s draft row is tinted `bg-callout` to mark a retro "In
  progress" — the tint is the only thing on the row saying so. The UI-polish
  round's new `hover:bg-canvas`, added uniformly across history rows as part
  of the milestone's hover-state pass, sits at the same specificity and
  overwrote it: pointing at the draft row made it indistinguishable from a
  hovered finished row, so the one moment a household reached for that row
  was the exact moment its status disappeared. The mechanism is the same
  cascade-order trap the grouped sidebar's Money links already hit (see
  pattern 3) — a later rule at equal specificity wins regardless of which
  state a reader would call more important — but the shape is worth naming
  on its own: a hover rule added everywhere, on the assumption that hover is
  always additive, silently deletes whatever state a row's own background
  was already carrying. Fixed by branching the hover class on the same
  condition that applies the tint, so the draft row hovers to a different,
  still-distinguishable colour instead of losing its own.

- **A panel built from the design file agrees with the design file and
  disagrees with the page it lands on.** M3's three defects were one shape
  three times over. Bills' "All caught up" had a card's radius and neither a
  card's border nor its background, so the one panel on the page that is good
  news read as loose text on the canvas. Finances' Net row carried a muted
  label and the same figure weight as the per-type rows it sums, so the
  conclusion read as another peer. Retros' empty detail panel put one muted
  sentence at the top of a card as tall as the history list beside it. Each
  was defensible in isolation and each was wrong beside its own neighbours,
  which is precisely what a design file cannot show you: it draws the panel,
  not the panel's siblings at the moment the panel joins them. **Review a new
  panel against the page it joins, not against the design it came from** —
  open the page, look at the element directly above it, and name the property
  that differs. All three of these are one glance apart from invisible.
- **Changing a `tabular` figure's font size undoes the alignment `tabular` is
  there for.** M3's plan called for the Net figure to go to `text-[15px]` to
  read as a total. Tried in a browser, it failed twice: the card's own `<h2>`
  is 14px, so a 15px total outranks the heading it sits under, and — the
  non-taste half — `tabular-nums` only lines a column up at *one* size, so the
  larger Net put its decimal point off the x-coordinate of every row it sums.
  That is the exact misalignment M1's walk had already fixed once on this card
  by bringing `tabular` forward. The fix was the label weight alone, which was
  the part of the change that carried the meaning. **A size change inside a
  column of aligned figures is a column change, not a type change** — the
  rule in `index.css`'s `.tabular` comment is about figures that stack, and a
  figure that stops matching its stack has left the stack.

- **A `<select>`'s widest `<option>` is a page width.** `/sign-up/$token` — the
  screen that creates the household — drew its 428px card unchanged on a 375px
  phone and scrolled sideways: `scrollWidth` 452 against `clientWidth` 375, and
  452 against 305 at the 320px floor this project promises not to scroll at.
  Nothing on the screen was wider than the card, and no rule in the file names
  a width bigger than the viewport. The width came out of the currency
  `<select>`: a select's min-content width is its longest option ("BAM —
  Bosnia and Herzegovina convertible mark"), and the card's wrapper is a **grid
  item**, whose `min-width` is `auto` — its own min-content width — which
  floors the auto-sized track it sits in. One option in a list nobody had
  opened set the width of the page. Blanking every option's text in the console
  dropped `scrollWidth` from 452 to 305, which is the entire diagnosis in one
  measurement. `min-w-0` on the grid item stops the propagation. **The trap is
  that it fixes only track sizing:** an unbreakable string — the address echoed
  by "Check your email", a household name on the invite screen — still lays
  itself out past the card and still counts toward `scrollWidth` (417 in a
  305px viewport, *after* the first fix), and needs `break-words` where that
  string is rendered. Two unrelated mechanisms push a phone sideways, and
  fixing one hides nothing about the other: **re-measure after the fix rather
  than reasoning that it was the fix.** Note also what a class check would
  have missed — `break-words` applied from the console computed to
  `overflow-wrap: normal`, because Tailwind only emits a class it has seen in
  the source; the browser measurement is what said so.
- **A width-matrix walk covers the screens that existed on the day it ran.**
  The mobile round walked 320/375/414/768/1024/1440 across every screen,
  sign-in and sign-up included, and its results are recorded in
  `docs/FEATURE_TRACKER.md`'s own Mobile-responsive row. The screen that broke
  was built afterwards, by a task whose subject was self-serve sign-up, on a
  desktop viewport; it reused the auth-card wrapper verbatim — correctly — and
  put the first `<select>` in the family inside it. Nothing regressed: the new
  screen was simply never in the matrix. **A completed walk is evidence about
  a tree, not a property the codebase now has** — the screen added after it
  needs the same widths run again, and the six copy-pasted wrappers around it
  all took the fix, not the one that was reported (pattern 1).
- **A shared header measured once does not stay measured as its own content
  grows.** The admin-households walk (2026-09-02) ran the operator header
  down to 320px and found no overflow — correct, for the two links (`Flags`,
  `Households`) `OperatorNav` carried that day. This task's own diff added a
  third (`Mail`) to that same shared header in `AdminShell.tsx`, and the
  outbound-mail walk's brief asked for one width narrower, 305px, which the
  earlier walk never ran. The header overflowed by 14px, on every
  `/admin/*` route — not only the two screens this task shipped — proved by
  turning `Mail`'s `display` off in the live page and watching
  `document.documentElement.scrollWidth` drop from 319 to exactly 305. This
  is the sign-up `<select>` lesson from the other direction: there the width
  came from one wide option nobody had opened; here it came from one more
  short link in a list that used to fit. **A shared component's fit is a
  property of its content at the moment it was last measured, and adding one
  more item to a list a nav renders is exactly the kind of change no
  class-level check (`make lint`, a component snapshot test) can see,
  because nothing about the new item is individually wrong — only the sum.**
  Fixed with `flex-wrap` on the nav (`gap-x-4 gap-y-1`, `justify-end` so a
  wrapped second row still hugs the right edge), trading "always one line"
  for "never wider than the viewport" — the shape that survives a fourth nav
  item arriving later without needing to be re-measured again, unlike a
  fixed width or a manually-verified gap value would.
- **A copy helper that returns a fragment makes every caller responsible for
  grammar, and the second caller will get it wrong.** `limitedAccessPhrase`
  returned a bare list — `"calendar & chores"` — and `"no"` when a limited
  member held nothing. Its first caller glued `" access only"` on the end, its
  second `" only"`. For a member with no capabilities the invite screen
  therefore read **"Joining as Kid — no access only."** and the Settings
  members list **"Kid · no only"**, and that state is not exotic: the invite
  modal's three capability toggles can all be switched off, so the product
  reaches it unaided. Every test in `MembersPanel.test.tsx` asserted the
  populated string and stayed green, because the empty list is the one case
  nobody writes a fixture for. Fixed by making the helper return the finished
  clause (`"calendar & chores only"` / `"no extra access"`) so neither caller
  composes anything — and the wording matters too: "no access" would have
  contradicted the line printed directly above it on the same screen, since
  Family is visible to every member regardless of capability. **When a helper's
  result only reads correctly for some inputs, the caller has to know which —
  return the whole thing, and test the empty branch, which is where the
  ungrammatical output always lives.**
- **An icon typed as a Unicode character is a bet that the device has a font
  covering it, and the phone in the household's hand is where that bet is
  settled.** Sign out was `⏻` (U+23FB POWER SYMBOL). On a Samsung Fold 7 it
  rendered as nothing: the navigation drawer's sign-out control was a blank
  button. It looked perfect on macOS throughout, because macOS ships a font
  with that glyph and Android does not — and the app's own webfont, Schibsted
  Grotesk, carries neither it, `☰` (U+2630, the only control that reopens
  navigation on a phone) nor `✕` (U+2715, every close and remove control). All
  three were leaning on whatever the device fell back to. Fixed by drawing
  them: `components/icons.tsx`, inline SVG on `currentColor`, `aria-hidden`
  because each control already carries its meaning in an `aria-label`.
  **The rule is the coverage, not the character:** `‹ › — ·` are ordinary
  Latin punctuation and safe anywhere, `▲ ▼ ✓` sit in blocks with broad
  fallback coverage and read beside their own labels, but a control whose
  entire visible content is a rare codepoint disappears when the bet loses,
  and disappears silently — nothing errors, nothing logs, and no test in a
  jsdom suite can see a missing glyph. Device coverage is not something a
  desktop review or a screenshot from the developer's own machine can check.
- **A read that is also a write inherits every hidden trigger of the read.**
  Every request under `/admin` writes an `admin_audit_log` row before its
  handler runs — reads included, on purpose (spec §2.4). `useAdminFlags` was
  a plain `useQuery`, and TanStack Query's default refetches a stale query
  whenever the tab regains focus. So every alt-tab back to the flags page
  was a logged "read flags", invisible until the audit screen existed to
  show the operator apparently reading flags dozens of times, and the audit
  page's own query would have done the same to itself. Found while building
  `/admin/audit` (2026-09-02); both reads now set `refetchOnWindowFocus:
  false`, with a test that flips `focusManager` and asserts the fetch count
  stays at one — watched fail first (two calls) on the flags page. The audit
  screen itself was descoped the same day; the fix to the flags read stayed,
  because the noise it stops is in the log whether or not a screen shows it.
  **When a GET has a side effect, list the client's implicit refetch
  triggers — focus, reconnect, mount, interval — and decide each one
  deliberately;** the library's defaults were chosen for reads that cost
  nothing to repeat.
- **TanStack Router's `Link` concatenates `activeProps.className` onto
  `className`; it does not replace it.** A nav link written as a full base
  class list plus an active override that repeated two of its utilities
  (`border-transparent` → `border-accent`, `text-muted` → `text-ink`) rendered
  both classes at once, and stylesheet order — not intent — decided the
  colour: the active link looked identical to the inactive one. The jsdom
  test asserting `aria-current="page"` on the active link passed throughout,
  because it tested the router's state, not what the classes painted (pattern
  3: the simulated environment cannot see a CSS conflict). Found on the
  browser walk of the audit screen (2026-09-02, since descoped) and fixed by
  splitting the looks across `activeProps` and `inactiveProps` on a shared
  base that names neither colour. **Put a utility in exactly one of base,
  active or inactive — never in two.**
- **TanStack Router's `from` takes a route's internal id, not its URL path,
  and the two stop being the same the moment a pathless route joins the
  chain — admin households, 2026-09-02.** `useSearch({ from:
  "/admin/households" })` and `useParams({ from:
  "/admin/households/$householdId" })` read correctly and are wrong:
  `authenticatedRoute` is pathless (`id: "authenticated"`, no `path`), so it
  contributes nothing to the URL — `<Link to="/admin/households">` is right —
  but it *does* contribute to the id chain these hooks key on. The only valid
  values are `"/authenticated/admin/households"` and
  `"/authenticated/admin/households/$householdId"`, which
  `tsc --noEmit --noErrorTruncation` will list for you. It compiled up to now
  only because every earlier `from:` in the file (`/sign-in/magic`,
  `/invite/$token`, `/sign-up/$token`) sits directly under `rootRoute` with no
  pathless ancestor. **The type registration is what turns this into a build
  error instead of an empty object at runtime** — keep `declare module
  "@tanstack/react-router" { interface Register ... }` in place, and read
  what `tsc` offers rather than pattern-matching the URL. Two smaller
  siblings from the same task: a `<Link>` to a route whose `validateSearch`
  returns non-optional fields requires an explicit `search` prop, and
  `react-hooks/rules-of-hooks` fires on `component: () => {...}` arrow
  functions that call hooks, because the rule keys on the function's own
  name — both fixed the way `signInMagicRoute`'s existing comment in the same
  file already documents.
- **This branch's browser walk ran 2026-09-02, Task 11 of
  `docs/superpowers/plans/2026-09-02-hearth-admin-households-verification.md`:
  15 of 15 criteria pass, with two caveats.** Criterion 7's caveat was a real
  product defect — pattern 13, above, carries the full account and the fix.
  Criterion 12's caveat was in the walk itself, not the product: its first
  pass tested the sign-in screen's own countdown copy, local component state
  that clears on any reload regardless of the underlying lock, rather than
  the drill-in's lockout callout this task actually built — a verification
  that checks the wrong surface can pass for the wrong reason, proving
  nothing either way. The walk was redone against the callout capable of
  failing, and it passed.
- **The outbound-mail inspector's browser walk ran 2026-09-04,**
  `docs/superpowers/plans/2026-09-04-hearth-outbound-inspector-verification.md`:
  **15 of 15 criteria pass, one real defect found and fixed during the walk
  (criterion 14 — see the shared-header entry earlier in this section for
  the full account and the fix), one caveat.** Criterion 8's caveat: the
  exact hyphen/underscore trailing-character case `outbox_links.go`'s own
  comment names as the actual risk could not be forced live, because this
  same walk's earlier magic-link requests had already spent
  `andreas@hearth.family`'s hourly rate limit (`magicLinkPerHourLimit = 3`)
  — verified instead at the unit level, where the domain test file already
  carries both cases by name. Also required a real-environment fix outside
  the product before the walk could safely start: `andreas@hearth.family`'s
  password on the shared dev box did not match the documented default,
  left over from an earlier walk's own password change — one more failed
  guess would have locked the household for 15 minutes, so it was reset
  through the sanctioned `make reset-password` path rather than guessed
  again.
- **An admin screen that owns its own 404 must read the RAW query error, not
  the gate-filtered one** — the invariant, stated as a value rule because the
  ordering rule it used to be stated as cannot be tested (pattern 2, 2026-09-04).
  `isAdminLayerFailure` counts `NOT_FOUND` among the failures `AdminGate`
  owns, so the filtered error is `null` for every 404: `isNotFound(inlineError)`
  is false every single time, and the screen's own "no such thing" branch
  never runs. Three screens depend on this — `AdminHouseholdPage`,
  `AdminMailMessagePage` and `AdminDatabasePage`'s row viewer — and on all
  three a 404 is the ordinary case ("no such household", "Mailpit no longer
  holds this message", "no table by that name"), not a sign the operator
  surface is gone. Getting it wrong renders a heading with nothing under it,
  no error and no data. `AdminMailPage.tsx` carried a comment saying the
  check comes *first* — true of the file and irrelevant to the program — and
  was reworded in the same change that added the third screen, which is the
  only one of the three whose comment stated the value rule from the start.

### Tooling and infrastructure

- The architecture lint **never enforced the rule it existed for**. Both branches
  only matched imports *within* the module, so third-party imports in
  `internal/domain` passed. Proven by planting `pgx` and getting exit 0.
- It also read `go list` inside a process substitution, where `set -euo pipefail`
  cannot see failure — so a module that did not compile reported a clean pass.
- `api` had no `depends_on: migrate`, so ordering worked only because the
  Makefile happened to run migrations first.
- The database URL appeared in three places plus a dead variable pointing at a
  different host.
- `make dev-local` could not work on a clean machine: nothing loaded `.env`, and
  `air` was only installed inside the image.
- `readCookie` split on every `=`, truncating base64 CSRF tokens. Every mutating
  request would have failed with 403.
- A brief told the implementer to add `--email` to `create-invite`, which
  already declares `fs.String("email", ...)` for the invitee's address. Two
  flags of the same name on one `FlagSet` panic **at runtime**, the first time
  the flag set is parsed — `go build` and `go vet` both pass. Renamed to
  `--inviter-email`, with the flag name threaded through so each caller's own
  error names the flag it actually means.
- `goose -dir <dir> status`, with no `GOOSE_DRIVER`/`GOOSE_DBSTRING` supplied
  either as flags or as environment variables, always prints goose's usage
  block and exits 1 — there is no driver-less mode that just lists local
  migration files, even though `-dir` alone looks like it should be enough to
  inspect a directory. A deployment plan's step assumed one existed and would
  have failed the same way for every operator who ever ran it. The production
  runbook documents the working form instead: supply the driver and migration
  directory (as flags, or as `GOOSE_DRIVER`/`GOOSE_DBSTRING`/
  `GOOSE_MIGRATION_DIR` in the environment) every time, `status` included.
- `restore.sh` called `psql` as a host binary, but the box's own provisioning
  step installs only Docker, `age` and `rclone` — no Postgres client. The
  symptom would not have shown up until the restore drill was run for real on
  the production box, by which point a dev machine's own leftover client had
  already made the script look fine everywhere it had been tried. Fixed by
  running `psql` through `docker run --rm -i --network host postgres:17-alpine`
  instead: the client version always matches the server that produced the
  dump, and the box needs no extra package.
- `config.Load()` is called before every `adminctl` subcommand runs (and
  before `cmd/api`'s own startup), so a `.env` with `SMTP_USERNAME` set and
  `SMTP_PASSWORD` blank — the shape `deploy/.env.example` used to ship —
  fails every one of `unlock-household`, `reset-password`, `create-invite`
  and `prune` with `SMTP_USERNAME and SMTP_PASSWORD must both be set, or both
  left empty`, an error that names neither the lockout nor the command that
  was actually run. Because `cmd/api/main.go` also calls `config.Load()` at
  boot, a *running* api proves nothing about a later `admin` invocation: the
  trap is reachable specifically in the window after a successful boot, when
  `.env` is edited again — a credential rotation, a partial edit — and
  `admin` is then run against the now-invalid file, exactly the moment a
  break-glass command is needed most. Fixed at the source: `.env.example` now
  ships both blank rather than a pre-filled username against an empty
  password, and `deploy/README.md` names the error for whoever hits the trap
  another way.
- **Caddy does not append to `X-Forwarded-For`; it replaces it.** Putting Caddy
  in front of nginx was designed on the documented, widely-repeated behaviour
  that a reverse proxy *appends* its peer to the header, and the whole argument
  for `real_ip_recursive off` was written on that premise. Measured, Caddy
  replaces the header outright, preserving a caller's list only for callers
  listed in `trusted_proxies` — of which none are configured. Two things make
  this worth remembering, and neither is the directive itself.
  **First, the shipped configuration was correct under either mechanism**, so
  nothing was broken and nothing failed: `real_ip_recursive off` takes the
  *last* `X-Forwarded-For` entry, and both appending and replacing put the true
  peer there. The defect lived entirely in the reasoning, which is the kind
  that survives every test and then misleads the next person to change the
  file — the pre-fix comment even ruled out a perfectly safe replacement proxy
  as unusable. By the time it was caught, the false premise had propagated from
  the design into the plan, into ADR 0002 and into `web/nginx.conf`'s own
  comments, so removing it took a measurement plus a class sweep — those three
  sites, with `docs/SYSTEM_DESIGN.md` and `docs/HANDOVER.md` corrected in the
  same pass for the adjacent error of writing the trusted range as a single
  address — rather than one edit.
  **Second, the mutation test that should have caught it could not.** Flipping
  `real_ip_recursive` from `off` to `on` leaves the result unchanged, because
  against a single-address `X-Forwarded-For` the first and last entries are the
  same value — the two settings only diverge on a header with two or more
  entries, which this topology never produces. That looks like a passing
  mutation and is not one. **A mutation that fails to flip the result is a
  signal to go and check the mechanism, not a pass**: it means the test cannot
  see the thing the directive controls, so the directive's justification is
  still unverified. This is the refinement of `proving-tests-can-fail` — a
  green test proves nothing until you have seen it fail, and a red one proves
  nothing either until you have checked it went red for the stated reason.
- **On this host, a bind-mounted source change does not reliably reach
  either dev-mode watcher, and `docker exec … cat` proves nothing about
  what the dev server is actually serving.** Verifying the retro
  open-action-count fix in a real browser (`NextRetroCard` reading
  `openActionCount`), the Overview page kept rendering only four cards —
  no fifth "Next retro" card, and `GET /api/v1/retros` never once appeared
  in `performance.getEntriesByType('resource')` after a clean reload, even
  though `me.data?.capabilities` genuinely included `marriage`. `docker
  exec hearth-api-1 cat .../retro_handlers.go` and the same for
  `hearth-web-1`'s `OverviewPage.tsx` both showed the current, correct
  file — the bind mount (`./api:/src`, `./web:/app`) was faithfully
  reflecting host edits. The discriminating check was fetching the module
  straight from the running dev server, bypassing the browser cache
  (`fetch('/src/features/overview/OverviewPage.tsx', {cache: 'reload'})`):
  it returned content with no mention of `NextRetroCard` or `hasMarriage`
  at all — Vite's own transform cache was stale. `air`'s container log told
  the same story from the other side: no `building...` line since the
  previous day, despite every watched file having a fresh mtime inside the
  container. Both `air` (backend) and Vite's own watcher (frontend) rely on
  filesystem-change notifications (inotify) that this host's bind mount
  does not reliably deliver — a known class of gap for virtiofs/gRPC-FUSE
  volume drivers, not anything about this feature's code. **A container
  having the right file on disk is not evidence its dev server has picked
  it up; when a live-reloading dev process goes quiet in its own log for
  longer than a save-and-check cycle should take, restart it
  (`docker compose restart api web`) before spending more time doubting the
  code.**
  **It happened again in M3, in the form that is harder to notice.** Five
  files were edited in one round; four of them reached the browser and one —
  `retroCopy.ts` — did not, so the page rendered the new markup with an
  *empty* string where the new copy belonged. Nothing looked broken enough to
  suspect the tooling: a hard reload did not fix it (the staleness is
  server-side, not in the browser cache), the type-check passed, and the file
  on disk was plainly correct, so the obvious reading was that the code was
  wrong. The same discriminating check settled it in one command —
  `curl -s http://localhost:5173/src/features/marriage/retroCopy.ts | grep …`
  returned a transform with the new key absent — and `touch` on the file was
  enough to make the watcher notice. **A partial miss is the dangerous
  shape**: when only one module in a change goes stale, the app still renders,
  the other edits visibly work, and the browser walk this repo requires before
  calling anything done returns a confident, wrong verdict. **On this host,
  `touch` every file you edited before trusting what the browser shows, and
  when one change in a batch appears not to have landed, `curl` that module
  from Vite before doubting the code.**
  **A third instance, Vision task 11: total silence, not a partial miss.**
  Fixing a measure row's mid-number wrapping (`whitespace-nowrap` added to
  `MeasureRow`, `PillarCard.tsx`) on a live `/marriage/vision` walk, a
  reload showed the exact same wrapping as before the edit — not "mostly
  fixed with one gap" this time, the fix simply never reached the browser
  at all. `docker exec hearth-web-1 cat PillarCard.tsx` showed the correct,
  edited file; the served DOM (`document.querySelector(...).outerHTML`)
  still carried the pre-fix class list. `docker compose restart web` (no
  `air` involved here, frontend-only) fixed it in one command, the same as
  the first instance. What is new this time: no Vite console output ever
  signalled a stale watcher — no "quiet log" to notice, because Vite prints
  nothing between HMR updates by default, so there was no absence of
  activity to read as a symptom, only the DOM's own class list disagreeing
  with the file on disk. The general rule already given above still covers
  it, but the tell is different: **compare what the browser actually served
  against the file, not just against your memory of what you changed** —
  `outerHTML`/`curl`, not "I re-read the file and it looks right."
  **A fourth instance, Vision task 15's own fifteen-criterion browser
  walk: the whole back half of a file, silently.** Opening the Edit-vision
  modal for the very first time in this walk, the "Pillars" heading and
  "+ Add pillar" button rendered as zero-width, zero-height elements —
  `document.querySelector('[data-testid="vision-modal-add-pillar"]')`
  resolved to a real `<button>` with `getBoundingClientRect()` reading
  `0×0`, because its own text content was empty, and so was its sibling
  `<span>`'s. `curl`ing the served module
  (`http://localhost:5173/src/features/marriage/visionCopy.ts`) settled it
  in one command: the entire `--- VisionModal (Task 12) ---` half of the
  file — `modalTitle` through `reloadAndDiscardChanges`, everything the
  modal needs beyond the page itself — was simply absent from what Vite
  was serving, though `cat`ting the file on disk showed it in full. Not one
  missing key this time (the second instance) and not total silence on a
  single component (the third) — an entire later section of one file,
  never picked up, on a container that had been running for two hours
  before this walk started. `docker compose restart web` fixed it in one
  command, the same as every prior instance, and the served module then
  matched the file byte for byte. **The size of the miss does not predict
  the size of the fix, and does not change the check**: `curl` the module
  Vite is actually serving before concluding a freshly-built screen is
  broken, however much of it looks wrong.

- **A secret leaked through an error nobody constructed: `http.NewRequestWithContext`
  returns a `*url.Error` that embeds the whole request URL, and Telegram's API
  URLs carry the bot token in the path.** `https://api.telegram.org/bot<TOKEN>/sendMessage`
  is not an unusual shape — it is how the Bot API is addressed — so any error
  built by wrapping the return of `NewRequestWithContext` (or of `client.Do`)
  hands the full bot credential to whatever logs it. The trigger is not exotic
  either: a token with a control character in it, such as the trailing newline
  a copy-pasted secrets file leaves behind, is enough to make URL parsing fail
  and the error fire on the very first send. **Downgrading `%w` to `%v` does
  not help**, and that is the part worth remembering: `url.Error.Error()`
  formats the URL into its own message, so the token is in the string under
  *any* verb. The only fix is to not put that error in the message at all —
  `client.go` builds its errors from the **method name** it was calling, never
  from the request or the URL, and says so at the line. **Two habits fall out.
  Before wrapping an error from a call you handed a URL, ask what the error
  type prints** — `url.Error`, `net.OpError` and `pgconn.PgError` all carry
  more than the sentence you expected. And **treat "the credential is in the
  path" as a property of the client, not of one call site**: it makes every
  error path in that file a disclosure path, so the rule has to be written
  once at the top of the file rather than remembered at each `return`.

- **A new file that was never `git add`ed passes every local check and fails
  only in CI, because the local tree has it and the checkout does not.**
  Commit `b7b2c7f` ("Add UI Icon Fix") replaced three Unicode symbols with SVG
  components in `web/src/components/icons.tsx` and imported that module from
  five files — but the module itself stayed untracked. `npx tsc --noEmit`,
  `npx vitest run` and the dev server were all green locally, because all three
  read the working directory. The images workflow builds from
  `actions/checkout`, which has only what was committed, so `tsc` there
  reported five `TS2307: Cannot find module './icons'` and the `hearth-web`
  image was never pushed. The failure surfaced on the production box, one step
  before it could do harm: `deploy.sh` refused with *"ghcr.io/oandrz/hearth-web:
  b7b2c7f… is not in the registry"* rather than deploying a half-built release
  — the guard doing exactly its job. **Two things follow. `git status` is part
  of the definition of done, not tidy-up afterwards**: an untracked file is
  invisible to `git diff`, to `git commit -a`, and to every test you run.
  **And a green local suite says nothing about the commit** — it says something
  about your disk. The check that matches CI is `git stash -u` (or a clone of
  the pushed SHA) before believing the build.
- **The dev `web` container's file watcher can silently stop noticing edits
  to a file it already served.** Mid-fix during the outbound-mail browser
  walk, saving `AdminShell.tsx` produced no Vite HMR log line at all, and
  `curl`ing the module's own dev-server URL
  (`/src/features/admin/AdminShell.tsx`) kept returning the pre-edit
  transformed source — confirmed the file on disk, and inside the
  container, already had the new content, so this was the watcher, not the
  bind mount. `docker compose restart web` fixed it, at the cost of the
  container's entrypoint re-running `npm install` before `vite` came back
  (about 80 seconds) — a plain process restart would not have been enough,
  since `npm install && npm run dev` is the whole entrypoint. **A source
  edit with no matching HMR log line is a stale dev server, not a wrong
  fix** — check the served module directly before spending time doubting
  the diff.
- **A screenshot tool can lie about what a page renders, and the lie can
  look exactly like the bug you are hunting for.** Claude in Chrome's
  `computer` screenshot/`zoom` actions, on this box, rendered the operator
  header's nav showing only its first item — `Flags` — with the other three
  links entirely absent, even after their color was forced to opaque red
  with a yellow outline directly in the page and re-captured. Every other
  signal available (`getBoundingClientRect`, computed styles, the
  accessibility tree) said all four links were present, laid out, and
  correctly styled. The tab's `window.devicePixelRatio` read `2.5` against
  an `outerWidth` of `594` — an inconsistent scaling setup particular to
  this sandboxed display. Confirmed as a capture bug, not a render bug, by
  opening the same signed-in route in a second, independent tool
  (Playwright MCP): all four links appeared normally there. **A visual
  defect this convenient — matching a known prior bug shape exactly (see
  criterion 2 in `docs/superpowers/plans/2026-09-02-hearth-admin-households-
  verification.md`) — is worth one cross-check in a second tool before it is
  written down as a repeat**, especially on an unusual display
  configuration; the deciding evidence here was a second screenshot
  pipeline, not more scrutiny of the first one.

### The first production deployment (2026-08-15)

Nothing in the product broke. Everything below was wrong in a document, in a
vendor's catalogue, or in an assumption — which is exactly the class of defect
no test suite can hold.

- **A document describing someone else's product goes stale without any signal,
  and the only place it gets checked is the moment money changes hands.**
  ADR 2 named a Hetzner `CPX11` in Singapore. Five days later, at the order
  form: `CPX11` no longer existed — Hetzner had renamed its shared-vCPU lines on
  15 June 2026, and the nearest successor `CPX12` is a *smaller* machine, so the
  rename moved specs and not just labels. Worse, the cheap `CX` line **is not
  sold in Singapore at all**, so the region the ADR chose cost `$19.61/mo`
  against Falkenstein's `$7.07` — *more than the AWS bill the whole ADR existed
  to escape*, for half the machine. The ADR's own instruction, "confirm the
  current figure at purchase rather than trusting this table", is the only
  reason this surfaced before the money went out instead of on the first
  invoice. **Write that instruction into anything that quotes a third party's
  price, model name or free-tier limit.**
- **"Equivalent" options were not equivalent, and one ping proved it.** Having
  moved to the EU, Helsinki looked interchangeable with Falkenstein — same
  price, same everything. Measured from the owner's actual network: Falkenstein
  195.5 ms with 2 ms of jitter, Helsinki 246 ms with 29 ms and a 307 ms worst
  case, consistent across two runs. Latency was the one thing the region change
  had just traded away, so giving another 45 ms of it away for nothing would
  have been the single mistake that move could not afford. **When a choice is
  cheap to measure and the cost of guessing lands on the thing you just
  sacrificed, measure it** — and record the command, because a number nobody can
  re-run becomes folklore.
- **A superseded decision leaves instructions standing in the documents it did
  not touch.** ADR 3 moved mail onto Mailpit on the box. `deploy/README.md` —
  the *only* file the box sparse-checks out, and therefore the only one an
  operator reads while standing on it — still said to verify a domain with
  Resend, set `SMTP_USERNAME=resend` and paste an API key, for credentials that
  by then could not exist. Worse than a dead instruction: the same section
  described leaving `SMTP_USERNAME`/`SMTP_PASSWORD` blank as a silent trap, when
  under Mailpit blank is the *correct* value — so an operator following the file
  would have invented credentials to escape a trap that had become the intended
  configuration. **When an ADR supersedes something, grep for the decision it
  replaced and fix every instruction, starting with whichever file the person
  doing the work actually has in front of them.** This is pattern 1 again:
  fixing the instance (writing ADR 3) did not fix the class.
- **"SSH key only" in a provider's console did not mean password auth was off.**
  Hetzner's Ubuntu image ships `PasswordAuthentication yes` via a cloud-init
  drop-in; attaching a key at creation only stops them mailing you a root
  password. `sshd -T` said so plainly and nothing else would have. The fix has
  its own trap: sshd takes the **first** value it sees and Ubuntu includes
  `sshd_config.d/*.conf` at the top, so the override has to sort ahead of
  `50-cloud-init.conf` — the `00-` prefix is load-bearing, not tidiness. Verified
  in both directions: key auth still works, password auth returns
  `Permission denied (publickey)`.
- **`docker compose ps` hides exited one-shot services, so the check that
  matters most is the one that silently passes.** `migrate` is the gate between
  a deploy and a corrupted-looking install: if it fails, `api` refuses to start
  and keeps serving the old container, but `web` depends only on `api`, so nginx
  comes up with the **new** bundle regardless — which presents as a frontend bug,
  not a stopped migration. Any automated check for it must use `ps -a`.
  `deploy/deploy.sh` does, and that is the single most valuable line in it.
- **A reboot is not a small deploy.** `depends_on:
  service_completed_successfully` is honoured by `docker compose up`, **not** by
  the daemon's restart policy. On reboot every `unless-stopped` container comes
  back in no particular order and `api` starts with no migration gate in front
  of it. Harmless with migrations already applied; not harmless on a box that
  reboots holding an image whose migrations never ran.
- **Two of the four hand-typed deploy commands failed silently**, which is why
  they became a script rather than a list. `sed -i "s/^IMAGE_TAG=.*/.../" .env`
  run from the wrong directory edits nothing, prints nothing and exits `0` — the
  deploy "succeeds" while serving the old build. And `IMAGE_TAG=latest` deploys
  nothing at all while every check stays green, because a migration-only change
  produces a byte-identical `api` image, so Compose does not recreate `api`, does
  not re-evaluate `depends_on: migrate`, and the migration does not run.
  `deploy.sh` refuses both **before** touching `.env`, and checks the registry
  first so a typo cannot leave the file pointing at an unpullable tag.
- **The walk's own tooling produced a false defect.** Setting `input.value` from
  a script is invisible to React's state, so the goals form correctly refused to
  submit and fired no network request at all — the values were in the DOM and the
  form still knew it was empty. Read as a product bug for two rounds. The fix is
  the native setter plus a bubbling `input` event. **A form that refuses to
  submit and sends nothing is more likely to be a harness problem than a
  product one**; check `form.checkValidity()` and the invalid-field list before
  concluding anything. (The actual cause the third time was simpler still: a
  `required` field left blank. Native validation worked.)
- **`CRON_TZ` is silently ignored by Ubuntu's cron, and a schedule that never
  fires looks identical to one that has not come round yet.** The crontab said
  `CRON_TZ=Asia/Singapore` with `17 3 * * *` and carried a confident comment
  explaining that this made "3:17am" mean local time. Vixie cron 3.0pl1 does not
  implement `CRON_TZ`; it treated the line as an ordinary environment assignment
  and scheduled the job for 03:17 **UTC** — 11:17 in the morning locally. The
  backup had never run.
  **Nothing surfaced this.** The heartbeat had been pinged by hand often enough
  that healthchecks.io looked healthy, the cron daemon was `active`, root's own
  hourly jobs were firing every hour in the log, and `crontab -l` showed exactly
  what was intended. The only way to tell was to look for the *absence* of a
  thing: no object in the bucket with a 03:17 timestamp.
  **Verifying a schedule needs the schedule, not the script.** The script had
  been run by hand, and then again under `env -i` with cron's exact `PATH` —
  both passed, and neither could have caught this. The test that did was
  scheduling a probe two minutes out under `CRON_TZ` and watching it not fire,
  then repeating it in plain UTC and watching it fire.
- **An alarm nobody has fired is not monitoring.** The uptime check was created
  and then deliberately proven by stopping `api` for four minutes and waiting for
  the alert to actually arrive. Same discipline as the escrow drill, and the same
  reason: the moment you need it is the worst possible moment to discover it was
  misconfigured. Before causing the outage, schedule an unconditional recovery
  (`systemd-run --on-active=8min …`) so a dead session cannot leave production
  down.
- **The plan asked for two monitors and only one got built.** healthchecks.io
  answers *did the backup run*; nothing answered *is the site up*. Both were in
  Task 7 step 10 and the gap survived a twelve-criterion walk, because the walk's
  criteria never mentioned monitoring — **a checklist only catches what is on
  it**, and the thing that was skipped was the one nobody had written a criterion
  for.
- **The same outage presents two different ways within a minute.** With `api`
  stopped, nginx first *hangs* (`HTTP 000`, no response) for ~40 seconds while it
  holds a cached upstream address for the container, then settles into a clean
  `502`. A visitor at 10 seconds sees a frozen browser; a visitor at 90 seconds
  sees an error page. Same fault, two symptoms, and they get diagnosed
  differently — which is how one incident becomes two bug reports.
- **The record has to describe the run that happened.** This walk's criterion 2
  was written up as "link deliberately left unopened" — and then the phone
  completed the flow, leaving a real test household in the production database.
  Corrected in place rather than deleting the row and leaving the sentence
  standing. **A verification document describing a tidier run than the real one
  is worse than no document, because it is believed.**

### Vision's fifteen-criterion browser walk (2026-08-29)

- **A later spec can assume a capability an earlier, sibling spec deliberately
  refused to build.** Criterion 8 reads "delete that goal from `/money/goals`"
  as though deleting a goal were an ordinary product action; Vision's own
  design doc treats it that way too, pinning `goal_id ON DELETE SET NULL` and
  a CHECK constraint's third branch specifically for "the state a deleted goal
  leaves behind." But Goals' own spec (`2026-08-01-hearth-goals-design.md`)
  says plainly, twice: "A goal archives; it is never deleted" and "Goals are
  **not** deleted and have no `DELETE` endpoint" — and the code agrees:
  `GoalRepository` has no `Delete` method at all, `router.go` wires no
  `DELETE /goals/{id}`, and `SetArchived`'s own comment says why ("there is no
  delete, the accounts precedent"). Nothing in Vision's own spec cross-checked
  this against Goals' — it was written three and a half weeks later, by
  someone who evidently assumed deletion was a normal goal operation because
  the schema and the CHECK constraint's own reasoning imply one exists.
  **The fix was not to build the missing endpoint** — that would override a
  deliberate, documented decision in a different feature for the sake of one
  criterion's literal wording — **but to exercise the same mechanism the
  criterion cares about (the CHECK's third branch, and the measure's
  broken-link render) through a raw SQL `DELETE`,** the identical shape
  Retros' criterion 10 used for a state its own product had no button for,
  and to name the gap plainly rather than pass over it. **When one feature's
  schema or tests assume another feature can do something, check that
  feature's own spec for an explicit "never" before assuming the assumption
  holds** — grep the sibling spec for the verb before building around it.
- **A whole-document editor can silently launder a domain state its own write
  path refuses to accept.** Vision's `measure_is_typed_or_linked` CHECK has a
  third branch — `goal_id`, `current_value` and `target_value` all `NULL` —
  that only a referential `SET NULL` can produce; the domain refuses to
  *create* that state on any `PUT` (spec decision 8's own words: "the domain
  still refuses to create one"). `VisionModal.tsx`'s own seeding effect
  (`useEffect` around `seededYear`) handles this correctly and says so in a
  comment: a "broken" measure loads into the editor as an editable typed
  measure, `current: 0, target: 1`, the least-surprising default given
  neither real mode has anything left to show. But that default is *silent*
  in the panel a household actually sees: editing this vision's theme alone
  and saving — never touching the Emergency-fund row — turned "Goal removed"
  into "0 of 1" on this walk, because the whole document, placeholder measure
  included, goes back on every save (spec decision 5). The household never
  chose "0 of 1"; the editor's own default did, and nothing in the modal
  flags that row as needing attention before Save is pressed. **Revised
  verdict, from the final whole-branch review: this is a latent defect, not
  an accepted trade-off.** The comment at the seeding effect is real and its
  reasoning about *which shape to land in* is sound — typed, blank-but-valid
  defaults beats a silently resurrected link — but that reasoning never
  addressed whether the *fabricated figure* should be allowed to leave the
  editor unresolved. It is dormant today for one reason only: Goals has no
  `DELETE` route and no `GoalRepository.Delete` (this same walk's own
  criterion 8 finding, above), so `MeasureBroken` cannot be reached through
  the running product at all — this walk needed a raw SQL `DELETE` against
  the database to produce it. Nothing here is a decision the household ever
  benefits from; it is a gap that has not yet had the chance to bite anyone.
  **The fix, when Goals gains a real delete, is not relaxing the domain** —
  "read tolerantly, write strictly" (`MeasureBroken`'s own doc comment) is
  correct and should stay — **but a third seeded state in the modal**: a
  visibly unresolved row, distinct from both typed and linked, that blocks
  Save until the household either picks a goal or types a number. Noted at
  the seeding effect in `VisionModal.tsx` as well, so whoever builds Goals'
  delete finds it from the code, not only from this log. Recorded here
  because the general shape recurs beyond this one feature: **a form that
  seeds itself from a state its own submission cannot represent needs to
  say so in the UI, not only in a code comment** — a household editing an
  unrelated field should not be able to fabricate a number by omission.

---

## Before you call something done

1. `make lint && make test` — both, on the tree you are about to integrate.
2. Mutate at least one new test: break the code, watch it fail *for the
   reason you expect*, restore. A mutation that kills a different test, or
   fails on the right test for the wrong reason, has not proven anything yet.
3. Grep for the shape of anything you fixed. Siblings are the norm here.
4. If it touches the browser, open a browser and walk it as a first-time
   user would, not only against criteria the spec wrote down — a 15-of-15
   walk scripted from the spec still missed the spec's own wrong decision,
   a silent navigation gap and unexamined hardcoded copy (pattern 13).
5. If it accepts caller input, ask what a caller can measure.
6. If it writes twice, ask what happens when the second write fails.
7. Add what you learned to this file.
8. `git status` before you push. A file you created and never `git add`ed
   is present for every local check and absent from the commit — CI is the
   first thing that reads what you actually pushed.
