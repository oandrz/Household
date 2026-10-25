# Cashflow — monthly and yearly, with a light that says whether we're on track — design

Date: 2026-10-25. Source: brainstorming with the product owner, plus a
personal-finance specialist consulted during it. Mockups shown and approved in
the session: `cashflow-layout.html` (option A chosen) and
`cashflow-yearly.html`.

Calls marked **(builder's call, made in brainstorming)** were made by Claude,
not by the product owner. They saw each one and did not object, but they did not
choose it. Change one only with a reason.

## Why

The product owner judges whether the household is financially safe with one
rule of their own: *"if I still have money left each month to put into savings,
our cashflow is good; if it's zero or negative, it's bad."* Today they work that
out by eye. They want Hearth to do the sum for them, month by month and for the
whole year, and to tell them plainly whether things are still okay.

They are not a finance professional. The specialist confirmed their rule is the
right foundation and named the ways it misleads when taken literally. Most of
this design is about guarding against those.

**Their biggest worry**, in their words when asked which failure would bother them
most: the page saying things are fine when they aren't. Hearth only knows what
someone typed in, so a month where half the spending was never entered looks
wonderful. Every rule below that could show green has to survive that worry.

## Goals

1. **Money in, money out, left over — per month and per year**, for the whole
   household: both partners' income together, and all household spending,
   whoever spent it.
2. **A traffic light with a reason in words**, so a non-expert can trust it:
   green *on track*, yellow *watch*, red *at risk*, and grey *not enough data*.
3. **Honest about missing data.** A month that looks incomplete shows grey,
   never a hopeful green.
4. **No new data entry.** It works entirely from what the household already
   logs: transactions, bills and accounts.

## Non-goals

- **Budgeting advice.** No "you should cut dining out." Budget already exists
  for planning spending.
- **CPF.** Salary is whatever the household records as income. The page does
  not model CPF or CPF-paid mortgages.
- **Bank sync.** Hearth has none.
- **A credit-card debt check.** Hearth can't tell a card bill that will be paid
  in full from debt being carried, so the check would guess wrong
  **(builder's call, made in brainstorming)**.
- **Splitting salary from bonus.** The owner decided a month's income is
  *everything that came in*: salary, salary plus bonus, or a one-time windfall.
  Nobody labels income.
- **A card on the Finances page or on Overview.** Possible later, reading the
  same numbers. Not part of this.

## Anti-goals: it fails if...

1. **It shows green when data is missing.** This is the owner's top worry.
2. **Its totals disagree with the Transactions page.** If Cashflow says March
   spending was S$4,200 and the ledger adds up to S$4,350, they stop trusting
   both. Cashflow uses exactly the transactions the ledger shows. Where it
   deliberately differs (spread yearly bills, and loan repayments made as
   transfers; see below), the page says so on its face.
3. **Moving money counts as spending.** Moving money to savings or paying the
   credit card is not money leaving the household.
4. **The light flips colour every month** because of a yearly bill or a bonus.
5. **It sounds like a financial adviser.** The wording is factual: "on track by
   common guidelines", "you kept 12% of income". Never "your finances are
   safe", never a recommendation. In Singapore, product-specific financial
   advice is regulated, and these thresholds are general guidance.

## What the owner decided

- **Household-level.** Both partners' income counts together.
- **Income is everything that came in that month.** No salary/bonus split.
- **A page of its own: Money › Cashflow**, a new tab next to Budget, Goals and
  Bills, with **Monthly** and **Yearly** tabs. They chose this over a card on
  Finances (option B) because the month-by-month list, which you can click
  into, is how you spot a month with missing entries.
- **Grey is decided automatically** (rules below), with a **"Looks right,
  count it"** button on a grey month. They chose this over ticking each month
  as complete, and over doing both.
- **The light's rules** in the *Traffic light* section, as shown in the yearly
  mockup.
- **Nothing they dislike is missing** from the anti-goals list.
- **Loan repayments made as a transfer count as money out** (see *Money in
  and money out*). Decided after the builder check.

## How the numbers are worked out

All amounts are `int64` minor units in the household's primary currency.
Percentages are whole basis points (1,900 = 19.00%), computed with integer
arithmetic: no `float64` anywhere on this path.

### Money in and money out for one month

- **Money in** = every `income` transaction dated in the month.
- **Money out** = every `expense` transaction dated in the month, adjusted for
  bills as described in *Spreading yearly and quarterly bills*, plus every
  transfer into a `loan` account dated in the month.
- **Transfers** between the household's own accounts are neither, with **one
  exception, decided by the owner: a transfer into a `loan` account counts as
  money out.** Money paid to a loan is not money the household can save, which
  is the owner's own rule. A finance professional would count the part of each
  payment that pays down the loan as saving, so this light is deliberately
  stricter. Not counting it would make a household with a loan look about one
  repayment better off every month: anti-goal 1. Three details settle with it
  **(builder's call, made in brainstorming)**:
  - A transfer into a `credit_card` account never counts, because the card's
    purchases were already counted as expenses.
  - A transfer *out of* a loan account (borrowing) is not money in. Borrowed
    money is not income.
  - A loan set up as a Bill and marked paid writes an expense, so it counts
    once, as spending. The *"i"* panel says not to also log a transfer for
    the same payment.
- **Goal contributions** never count. They are a separate ledger that doesn't
  move any account, so the money is still counted where it actually sits.
- **Left over** = money in − money out. **Kept** = left over ÷ money in, in
  basis points. When money in is zero, Kept is undefined, and the month is
  grey anyway (no income recorded).

**Currency.** Every transaction converts into primary *before* it is added,
using `NewConverter` / `TryConvert`. This is the rule
`TransactionService.MonthSummary` and Budget's `tallySpend` already follow
(LEARNING pattern 12). A transaction with no rate is not silently dropped. Its
month goes **grey** with the reason "a S$… / Rp … transaction couldn't be
converted", following `MonthSummary.ExcludedNoRate` and Net worth's
`Computable` / `ExcludedAccount`. A quietly short total looks identical to a
correct one.

**Dates.** A month is a calendar month in the household's own timezone, the
same boundary the ledger and Budget use. A transaction dated before its
account's opening balance still counts: the money was spent. This matches
`MonthSummary`.

### Spreading yearly and quarterly bills

The aim: a S$2,100 insurance premium paid in December shouldn't make December
look terrible and the other eleven months look better than they are.

- Recording a bill payment (`BillRepository.RecordPayment`) writes an expense
  transaction linked to its `bill_payments` row.
- For a bill whose cadence is **quarterly or yearly**, each recorded payment's
  linked transaction is **left out** of its own month. Instead, **the amount
  actually paid is spread forward across the period it covers**: a yearly
  payment over 12 months, starting with the month it was paid; a quarterly
  payment over 3 months, starting the same way. Spread with integer
  division, and put the remainder in the first month, so the parts add up
  exactly to what was paid.
- **Monthly** bills count as normal, through their linked transactions.
- One-off bills (bills that don't repeat) count as normal too.

Why spread actual payments rather than the bill's current amount: every
dollar paid is counted exactly once, in the months it covers. A backdated
payment, a bill created after its first payment, a bill archived later, and
a premium that changed over the years all add up correctly with no special
cases **(builder's call, made after the builder check)**. An earlier draft
spread the bill's current amount from its creation month. The builder check
showed that this loses money in the normal case: a bill created in October
and paid in December would drop S$1,575 of its first premium. That loss
flatters the household, which is anti-goal 1.

What this means for the edge cases:

- **Before a bill's first recorded payment, no month carries it.** The first
  year with a new yearly bill shows the cost only from the payment month
  onward. That is honest: it reflects what was actually paid.
- **A payment's spread can run into months that haven't happened yet.** Those
  months show it when they arrive. It doesn't affect "Heading for", which is
  projected from the 3-month average.
- **A yearly cost logged as a plain expense, with no bill behind it, is not
  spread.** That is a known gap: the page can only spread what it can see is a
  bill. The month carries the lump as it was entered. The *"i"* explanation
  says so, and suggests entering yearly costs as bills.

Because of this, Cashflow's monthly money out **differs from the ledger's
"Spent this month"** in months with yearly or quarterly bills. The Money-out
figure must show the adjustment on its face, for example "includes S$310 of
yearly bills spread across the year", and the month's drill-down must list
which bills contributed. Loan transfers get the same treatment: "includes
S$1,800 of loan repayments", listed in the drill-down. These two are the only
allowed differences from the ledger (anti-goal 2).

### Not enough data (grey)

A finished month is grey when any of these is true:

1. **No income was recorded** in the month.
2. **Spending is less than half of usual.** *Usual* is the median money out of
   the household's last 6 *solid* (non-grey) finished months. This rule only
   applies once there are at least 3 solid months. Before that, only rules 1
   and 3 apply **(builder's call, made in brainstorming)**.
3. **A transaction couldn't be converted** into the primary currency.

**"Looks right, count it"** clears rules 1 and 2 for that one month. It does
not clear rule 3, because a missing exchange rate is a fact, not a judgement.
It is stored, so both partners see the same answer. Anyone who can open the page (a household owner, per the route guard) may
press it, and may undo it. An undo button sits in the same spot.

A grey month **drops out of every average**. If one of the last three
finished months is grey, the "3-month average" is the average of the other
two, and the page says "average of 2 months". If all three are grey, there is
no average, and the light is grey.

### The current month

The current month is unfinished. It shows **"so far"** figures with no colour,
and is **left out** of every average and of the light. Otherwise every month
would look great on the 25th, before the rest of its spending arrived.

### Cash cushion

**Cash cushion (months)** = the sum of current balances of non-archived
accounts of type `cash` ÷ the average money out of the last 3 solid finished
months. It is shown to one decimal place, computed in integer tenths.
Investment, property, loan and credit-card accounts don't count, because you
can't spend them quickly. Accounts marked as not counting toward net worth
are left out too. They are the same "not household money" accounts Net worth
skips (`count_toward_net_worth`). Their *transactions* still count in money
in and out, so the totals match the ledger. Balances convert into primary the
same way Net worth does.

The cushion can't always be worked out **(builder's call, made after the
builder check)**:

- **An account can't be converted**, or **there are no solid months to
  average**: the cushion shows "—" with the reason, rather than a short
  figure.
- **The household has no `cash` accounts at all**: the cushion shows "—" with
  "no bank or cash accounts recorded". It is not shown as 0 months, because
  Hearth doesn't know the balance; it isn't zero.

When the cushion is "—", the light **cannot be green**. Its best colour is
yellow, with the reason ("cash cushion couldn't be worked out"). It is not
red, because red would claim a danger Hearth can't see.

## Traffic light

Every check runs, and the **worst result sets the colour**. The page shows the
reason in words next to the colour, for example: "Watch — you kept 6% of
income on average over the last 3 months." The light judges the **last
finished month and the 3-month average**, never the current month.

| Colour | When |
|---|---|
| **Grey: not enough data** | The last finished month is grey and has no "count it", or there is no solid month in the last three to average. |
| **Red: at risk** (any) | The 3-month average left over is **zero or negative** (the owner's own rule) · cash cushion **under 1 month** |
| **Yellow: watch** (any) | 3-month average Kept **under 10%** · the last finished month was **negative** · cash cushion **1 to under 3 months**, or **"—"** (couldn't be worked out) · a **quarterly or yearly bill due within 60 days** is larger than the 3-month average left over (a cash-timing warning: the money must exist on the day, even though it is spread for judging) |
| **Green: on track** (all) | 3-month average Kept **10% or more** · cash cushion **3 months or more** · nothing above fired |

Thresholds come from Singapore's MoneySense general guidance (save at least
10% of income; keep 3 to 6 months of expenses as an emergency fund). They are
the same for every household, and the *"i"* panel says they are general
guidelines, not advice.

The bill-due check reads each live quarterly and yearly bill's `next_due`.

## Yearly view

Calendar years. A year picker steps back to the oldest year with any
transaction.

- **Kept so far** = the sum of the year's finished months' left over (using
  spread bills). It is shown next to **the same months of last year**, never
  against last year's full total. Grey months are included in the sum, but
  the figure is marked "includes N months with not enough data".
- **Heading for (full year)**, only for the current year = kept so far + the
  number of remaining months, including the current one, × the 3-month average
  left over.
  **Correction to the approved mockup:** the mockup also subtracted "yearly
  bills still due". That would charge them twice, because the spread months
  already carry their share. There is no subtraction. For the same reason the
  mockup's December bar, "lower because insurance is due", was wrong: with
  spreading, December is not lower. The owner should be told about this
  correction when they read this document.
- **For a finished year**: the total kept, the share of income kept, and the
  comparison with the year before.
- **Chart**: one bar per month. Finished months are solid, grey months are
  striped, the current month is pale, and projected months are dashed. Bars
  above a zero line are green; bars below it are red.
- **Bills still to come this year**: the live quarterly and yearly bills whose
  `next_due` falls before 31 December, with amount and month. This list is
  information. It does not change "Heading for".

## Monthly view

As approved in `cashflow-layout.html` (option A), with the corrections above:

- The light and its reason.
- Three figures for the last finished month: money in, money out (with the
  spread-bills note) and left over (with Kept %). The 3-month average sits
  underneath.
- The current month as a "so far" line, without a colour.
- A 12-month bar chart of left over.
- A month-by-month table: month, in, out, left over, kept, light. Clicking a
  month opens the transactions behind it. If the ledger can be opened filtered
  to that month, link there; otherwise use a drill-down. Either way, it lists
  the spread bills that month carried. A grey row says why ("only 6 entries",
  "no income recorded") and carries the "Looks right, count it" button.

**Empty and sparse states.** With no transactions, the page explains what it
needs ("log income and spending for a month to see cashflow") and links to
Add transaction. With fewer than 3 finished months, the light still works, but
the average is labelled with how many months it covers.

## Constraints from the codebase

- **Authorisation lives at the HTTP edge** (CLAUDE.md, ADR 8). The page's read
  route(s) and the "count it" write sit behind the same guard as
  `/money/transactions` and `/money/portfolio` (household membership, money visibility **and the owner role**: `requireOwner`
  in `router.go`). No service takes an actor.
- **Every 2xx except 204 carries a JSON body.**
- **Fail closed** on any value read from a column or request: transaction
  kind, bill cadence, account type.
- **ADR 13 and ADR 14** (per-household limits on stored rows and request time
  limits). The confirmation table is tiny (at most one row per month), but it
  still needs a ceiling under ADR 13. A full history read must stay inside
  ADR 14's request time limit; scanning a household's every transaction on
  each page load needs checking against it.
- Clean architecture: the arithmetic (spreading, grey rules, light) is pure
  logic over values. It belongs in `internal/domain` or a usecase, unit-tested
  against in-memory doubles. It does not belong in SQL or React.

## What's left to the builder

- **Structure goes through `hearth-architect` first** (CLAUDE.md): whether this
  is a new `CashflowService` or an extension of an existing one, the new table
  for "count it" confirmations (household, month, who confirmed, when), its
  migration and port, and whether history is computed on each read or cached.
- The API shape: one endpoint returning the whole page, or separate monthly
  and yearly reads.
- Exact wording of every reason line and the *"i"* panel, within anti-goal 5.
- Chart construction. Net worth and Portfolio both hand-build inline SVG with
  no charting dependency; follow them.
- Whether the month drill-down reuses the ledger's filters or is its own view.
- Mobile layout.
- `docs/FEATURE_TRACKER.md`: new rows under Money for this page (monthly view,
  yearly view, light, grey rule with "count it", cash cushion), with the
  summary recounted. Also `docs/SYSTEM_DESIGN.md` and `docs/LEARNING.md`, as
  CLAUDE.md requires.

