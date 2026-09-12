# Investment portfolio tracking

## Problem

A Hearth household holds money in more than one kind of investment — listed
stocks, physical gold, whatever else — and today the product can only record
each as an **account with a balance**. A balance moves for two completely
different reasons: the market moved, or the owner put more money in. Hearth
cannot tell those apart, so it cannot answer the only question an investor
actually asks: *which of these is earning its place?*

The cost of leaving it unsolved is that every allocation decision — where the
next contribution goes — is made from memory or from a spreadsheet kept outside
the product. Net worth is also quietly wrong between the moments someone
remembers to retype an investment balance.

## Evidence

- **Assumption — the product owner's own use.** No user quotes, tickets or
  analytics exist for this; it is the owner describing his own portfolio.
  Validate via one real quarter of entry: if the owner does not keep the
  holdings current for three months, the report has no inputs and the
  hypothesis is unproven rather than disproven.
- **Confirmed in the code, not assumed:** `domain.AccountInvestment` exists
  (`api/internal/domain/account.go:18`) and carries no units, no cost basis and
  no market price. An investment account's balance is its opening figure plus
  ledger transactions, the same as a cash account.
- **Confirmed in the design:** `design/Household Dashboard.dc.html` draws
  "Investments & CPF" only as a slice of the net-worth breakdown. **No portfolio
  screen is drawn anywhere in the design.** This is a feature the design does
  not describe, so it needs new `docs/FEATURE_TRACKER.md` rows rather than
  filling existing ⬜ ones.

## Users

- **Primary** — the **household owner or a member holding the `money`
  capability**, at the moment they are deciding where the next contribution
  goes. The trigger is periodic (a quarter closing, a bonus arriving), not
  daily; this is a review surface, not a trading screen.
- **Not for** — **limited members without the `money` capability.** Holdings,
  quantities, cost basis and every derived figure must be redacted at the wire,
  the way Accounts already redacts amounts. A member who cannot see a bank
  balance must not be able to see that the household holds 300g of gold.
- **Not for** — anyone wanting live prices, trading, or tax reporting. This
  reports on a period that has closed; it is not a ticker and not a tax return.

## Hypothesis

We believe **a per-instrument profit-and-loss report over quarter, half-year and
year** will let a household owner **see which instruments actually earned money,
separated from the money they put in** for **owners who hold more than one kind
of investment**.

We'll know we're right when **the owner can name their best- and worst-performing
instrument for a closed quarter, from Hearth alone, without opening a
spreadsheet — and the answer changes where the next contribution goes.**

## Definitions

These are requirements, not implementation. Every one of them is a number the
design does not specify, and the `hearth-product-driver` skill states the rule
for exactly this situation — *"the design displays `66% used`, `S$137/day left`
… and specifies none of them. Pin every formula in the spec or each implementer
will invent one."* So they are pinned here.

**Profit for a period = total return, reported as three separate figures that
sum to a total:**

| Component | What it is |
|---|---|
| **Unrealised** | Change in market value of what is still held, over the period |
| **Realised** | Gain or loss on positions sold during the period, against their cost basis |
| **Income** | Dividends, coupons, distributions received during the period |

- **Contributions and withdrawals are excluded from profit.** Buying more of
  something must never read as profit. This is the single most important rule
  in the feature — it is the exact failure of the balance-only model this
  replaces.
- **Fees and commissions reduce profit** in the period they are charged.
- **Cost basis is average cost**, not FIFO. Buying the same stock twice at
  different prices gives one blended cost per unit. FIFO would produce a
  different realised figure and a tax authority would want it — but this
  feature is explicitly not for tax (see Out of scope), and average cost is the
  only method that stays meaningful for a divisible holding like gold, where
  "the first gram" is not a thing that exists.
- **A holding acquired inside the period starts from its cost on the
  acquisition date**, not from a period-start valuation it cannot have. A
  holding sold inside the period **ends at its sale price**, at which point its
  unrealised gain becomes realised. Without these two rules the blanking rule
  below would blank every newly bought holding — the most common case there is.
- A period with **no valuation at an end it should have** produces **no figure
  and a reason**, never a zero — a holding held right through a quarter with no
  price recorded at either end is unknowable, not flat. Hearth already has this
  rule: the net worth card blanks and says why when a primary-currency change
  strands an account.

**Currency — every figure is reported in the household's primary currency, with
the instrument's own currency shown beside it.** A US stock that gained 5% in
USD while SGD gained 6% against USD made the household *poorer*; the primary
figure must say so. The native figure sits beside it so the owner can still tell
whether the *pick* was good and the currency was the problem.

**Periods** — calendar quarters (Q1–Q4), calendar half-years (H1, H2) and the
calendar year. Relative to the household's calendar, since no household in
Hearth stores a timezone today (`api/internal/usecase/account.go:165` and
`api/internal/adapter/postgres/transaction_repo.go:318` both record this).

**Instruments in the MVP** — listed stocks, gold, and a generic "other" valued
by hand. Bonds, crypto, funds and fixed deposits are not enumerated until asked
for; "other" carries them without a schema decision.

**Fractional quantity is a requirement.** 300.5 grams of gold and 0.5 of a share
are both real. Money in Hearth is `int64` minor units plus an ISO 4217 code and
`float64` never appears in a monetary path — **quantity is not money**, and how
it is represented is a question `/plan` must answer explicitly rather than
inherit.

## Success Metrics

| Metric | Target | How measured |
|---|---|---|
| Owner names best/worst instrument for a closed quarter from Hearth alone | Yes, on the first real quarter | The owner does it, once, on real data — the browser walk this project requires before "done" |
| Instruments whose valuation is current at period close | 100% of held instruments | Count of holdings with a valuation dated in the closing period, over holdings held |
| Net worth headline matches the portfolio's own market value | Always equal | Both figures read on one screen in the same session |
| Time to enter one quarter's valuations by hand | TBD — needs validation via the first real quarter | Owner-reported, once there is something to time |
| Contribution decisions the report changed | TBD — needs validation via owner interview after two quarters | Ask; there is no instrumented way to see this |

## Scope

**MVP** — a household with the `money` capability can record what it holds, per
instrument, with what it cost and when it was acquired; record a valuation for
each instrument as of a date; record sales and income against a holding; and
read a report showing unrealised, realised and income profit per instrument for
a chosen quarter, half-year or year, in primary currency with the native figure
beside it. The portfolio's market value is what the investment account
contributes to net worth. All of it manual — no external data source.

**Out of scope**

- **moomoo (and any broker API)** — deferred to milestone 4, not cut. Gold has
  no broker feed, so manual valuation must exist regardless; the report is what
  tests the hypothesis and the API only removes typing. Three things are
  unverified and must not be guessed at — see Open Questions.
- **Live prices / intraday values** — this reports closed periods. A price
  refreshed on every page load is a different product with a different cost.
- **Tax lots, wash sales, tax reporting** — cost basis here is for "did this
  earn money", not for a tax authority. Say so in the product, so nobody files
  from it.
- **Benchmark comparison (vs STI, vs S&P)** — genuinely useful for "which
  instrument to focus on", but needs an index price source, which is the same
  unsolved dependency as live FX. Revisit once a rate/price source exists.
- **Trading, order placement, rebalancing advice** — Hearth is a record, not a
  broker, and advice carries a regulatory question this product has not asked.
- **CPF** — the design groups it with investments, but CPF has its own rules,
  its own statements and no market price. It is an account with a balance today
  and stays one.

## Dependencies

**A live FX rate source is a hard dependency for any non-SGD, non-IDR
instrument, and it does not exist today.** `api/internal/adapter/fx/static.go`
holds exactly one pair — `SGD↔IDR`, fixed at 12,410 — and the
`usecase.FXRateProvider` port is documented as converting "between the
household's primary and secondary currencies". A USD-listed stock cannot be
converted to a household's primary currency by anything in this codebase right
now. Either a live provider ships with this feature, or the MVP is restricted to
instruments priced in a currency the household already holds — that choice is an
Open Question below, and it changes the MVP's size materially.

**The `money` capability gate** already exists and is used by every Money route;
this feature adds no new authorisation concept, only more routes behind the same
gate. Authorisation stays at each channel's inbound edge
([ADR 8](../../docs/adr/0008-authorisation-at-each-channels-inbound-edge.md)).

## Delivery Milestones

<!-- Business outcomes, not engineering tasks. /plan turns each into a plan. -->
<!-- Status: pending | in-progress | complete -->

| # | Milestone | Outcome | Status | Plan |
|---|---|---|---|---|
| 1 | Holdings exist | The owner can record what the household holds — instrument, quantity, what it cost, when acquired — and see the portfolio's current value from valuations they enter. Replaces "an investment account is a number I retype." | complete | [`.claude/plans/investment-portfolio-tracking.plan.md`](../plans/investment-portfolio-tracking.plan.md) |
| 2 | The period report | The owner picks a quarter, half-year or year and reads profit per instrument, split into unrealised, realised and income, in primary currency with native beside it, **and a bar chart comparing those periods over time** (owner's request, 2026-09-12). Income and fees become recordable here, since profit cannot be reported without them. **This is the milestone that tests the hypothesis** — everything before it is input and everything after is convenience. | complete | [`.claude/plans/investment-portfolio-tracking-m2.plan.md`](../plans/investment-portfolio-tracking-m2.plan.md) |
| 3 | Net worth tells the truth | The investment account's contribution to net worth is the portfolio's market value, so the headline figure and the 12-month trend move when the market does, not when someone remembers to retype a balance. | pending | — |
| 4 | Stock positions arrive without typing | The owner connects a moomoo account and stock holdings, prices and (if available) trade history land in Hearth without manual entry. Gold and "other" stay manual. **Gated on the Open Questions below** — this milestone may not survive them. | pending | — |

Milestones 1 and 2 are the feature. 3 is what stops Hearth telling two
different stories about the same money. 4 is the one the owner asked for by
name and the one least certain to be buildable — which is exactly why it is
last.

## Open Questions

- [x] **Does the MVP allow instruments priced in a currency the household does
      not hold?** **Resolved — yes.** Milestone 1's Decision A settled it: every
      money-bearing row stores the native amount **and** the primary-currency
      amount the owner supplies, mirroring `Transaction.ReceivedAmount`. No rate
      is stored and no dated-rate port was invented, so a live FX source is a
      convenience that pre-fills a field rather than a correctness dependency.
      The single `SGD↔IDR` pair in `adapter/fx/static.go` is therefore not on
      this feature's path at all.
- [ ] **moomoo: is it a hosted HTTP API, or does it require a locally-running
      gateway process?** `TBD — needs validation via moomoo OpenAPI
      documentation.` If it needs a persistent local gateway, it fights the
      single-VPS hosting shape the product is committed to, and milestone 4 may
      need rethinking or dropping. **Resolve this one first** — it is the
      cheapest question that can kill a whole milestone.
- [ ] **Does moomoo return trade history and cost basis, or only current
      positions?** `TBD — needs validation via moomoo OpenAPI documentation.`
      Positions-only means cost basis stays hand-typed forever, which removes
      most of milestone 4's value.
- [ ] **What moomoo account tier and region is required, and what does it cost?**
      `TBD — needs validation via moomoo documentation and
      docs/INFRASTRUCTURE.md's own standard for recording an external
      dependency.` Any external service Hearth depends on gets a row there.
- [ ] **Does connecting a broker revive the design's dead "Link account — step 1,
      choose source" chooser, or add a separate surface?** The tracker records
      that chooser as permanently dead because SGFinDex is restricted to
      licensed institutions and a chooser with one dead branch teaches nothing.
      A broker connection gives it a live branch. Reviving it is the cheaper
      story; confirm before milestone 4.
- [ ] **How does a valuation get a date it can be trusted at?** A price typed
      today for "as of last quarter-end" is not the same claim as a price
      recorded at the time. Does the product distinguish them, and does a stale
      valuation expire?
- [ ] **Does an investment account hold uninvested cash alongside its holdings,
      and is buying a holding a *conversion* of that cash rather than a
      contribution?** This is the one that decides milestone 1's shape, and it
      is a requirements question, not a design one. Today a contribution to an
      investment account **is a ledger transfer** — that is how Budget and the
      12-month net worth trend see it. If holdings alone define the balance,
      that transfer's credit side either lands in uninvested brokerage cash or
      disappears from net worth until something is bought. Leaving it implicit
      is exactly how "buying more looks like profit" sneaks back in through the
      ledger door, which is the failure this whole feature exists to fix.
- [ ] **What happens to an existing investment account's balance when holdings
      arrive?** Households already using `AccountInvestment` have a number in
      it. Milestone 3 must not silently double-count or silently erase it.
- [ ] **Is gold entered as weight or as value?** 300g at a price per gram is the
      only way to get unrealised profit; "my gold is worth S$X" collapses to
      today's balance-only model. Confirm the owner will enter weight.

## Risks

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| **Manual valuations decay.** Nobody types prices after month two, the report goes stale, the feature dies quietly while still looking alive. | High | Fatal to the hypothesis | The report must show valuation age and refuse to compute rather than compute from stale data; measure "holdings with a current valuation" as a headline metric, not a footnote |
| **No FX source for non-household currencies.** Most listed stocks price in USD; nothing in the codebase converts USD. | Certain (it is the current state) | Blocks most stock holdings | Decide the first Open Question before `/plan`: restrict MVP currencies, or scope a live rate provider into milestone 1 |
| **moomoo needs a local gateway process.** Would not fit the single-VPS hosting shape the product is committed to. | Unknown — unverified | Removes milestone 4 | Verify against moomoo's own documentation before any moomoo work starts; milestone 4 is last precisely so this can kill it cheaply |
| **Fractional quantity invites `float64` into a monetary path.** Quantity × price produces money, and this codebase bans `float64` from money paths. | Medium | Silent rounding errors in the headline figure | Name the quantity representation explicitly in `/plan` and prove the multiply is exact. Not hypothetical here: `docs/LEARNING.md:3522` records the 50/30/20 budget split computing `incomeMinor * 0.3` and landing a pool one minor unit low, because `333333 * 0.3 === 99999.90000000001` |
| **Net worth regression.** Milestone 3 touches a shipped, walked, 12-month-trend feature that derives every figure from the ledger with nothing stored. | Medium | Breaks a working headline | Milestone 3 is separate from 1 and 2 so the report ships without touching net worth; walk net worth in a browser after, not just its tests |
| **Holdings leak to limited members.** A new surface with a new shape is a new chance to forget the redaction Accounts already does. | Medium | Privacy breach inside the household | Wire-level redaction, not UI-level; a test that a limited member's response body contains no quantity or figure |
| **Scope creep into a trading app.** Prices, charts, benchmarks and alerts all feel adjacent. | High | Feature never ships | The Out of scope list above is the defence; "closed periods only" is the line |

---
*Status: DRAFT — requirements only. Implementation planning pending via /plan.*
