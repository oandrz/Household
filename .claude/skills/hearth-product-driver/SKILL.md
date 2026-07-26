---
name: hearth-product-driver
description: Own the Hearth roadmap — what exists, what is next, why that order, and what must be settled before a slice starts. Use whenever anyone asks what to build next, whether something is already built, what is left, how much is done, or wants to start a new area of the product. Also use when a feature is finished, to record it. This is the skill that keeps the feature tracker honest and stops the product being built out of order.
---

# Driving Hearth

Hearth is a shared household dashboard, built from `design/Household
Dashboard.dc.html`. The design shows the whole product; the code contains two of
its six areas. Your job here is to know the difference precisely, keep it
recorded, and answer "what next" with a reason rather than a preference.

## Where the truth lives

Read these in this order. Do not answer from memory — the whole point is that
this changes.

| | |
|---|---|
| `docs/FEATURE_TRACKER.md` | Every feature and its state. The authoritative answer to "is X built?" |
| `docs/HANDOVER.md` | Current state, what to settle before the next slice, open items |
| `design/Household Dashboard.dc.html` | What the product *is*. Section `6a` is a flow map of every screen and modal |
| `docs/superpowers/specs/2026-07-26-hearth-foundation-design.md` | The slicing and the decisions behind it |
| `docs/LEARNING.md` | Defects and their patterns — read before planning work in an area that has bitten before |

If the tracker and the code disagree, the code wins and **the tracker is wrong
and must be fixed in the same breath**. A tracker nobody trusts is worse than no
tracker.

## The direction

Six slices. Two done. The order is dependency, not preference:

```
0 Skeleton ✅ → 1 Identity ✅ → 2 Money → 3 Marriage → 4 Family → 5 Overview
```

- **Identity came first** because everything is household-scoped and
  capability-gated. Repository signatures could not be written correctly without
  it.
- **Money is next** — the largest area, the design's centre of gravity, and
  nothing blocks it.
- **Marriage** is independent of Money. Agreements is the interesting problem:
  changes go through propose → both sign, with history preserved so a removed
  agreement can still be seen and restored. That is append-only and versioned,
  not CRUD.
- **Family** needs Bills, because bill dates appear on the calendar grid.
- **Overview is last** because it only aggregates the three above. It looks like
  screen one and building it early means stubbing everything it reads.

When someone proposes taking these out of order, say what it costs rather than
refusing. Overview before Money is not forbidden — it just means building it
twice.

## Answering "what's next"

Give the next slice, the reason, and what has to be settled first. Concretely,
for Money that is:

1. **Define the derived figures.** The design *displays* `66% used`, `S$137/day
   left`, `on pace to save S$1,780`, `4 of 4 on track`, net worth from assets
   minus liabilities, and unspent budget rolling into a nominated goal at month
   end — and specifies none of them. Pin every formula in the spec or each
   implementer will invent one.
2. **Wire `requireCapability`.** The middleware exists and no route uses it, so
   the promise that the server enforces capabilities independently of the UI is
   currently vacuous. Money adds the first capability-gated route.
3. **Accept that bank sync is a port, not a feature.** SGFinDex is restricted to
   licensed financial institutions. The design's Singpass flow shows as
   unavailable; accounts arrive by manual entry or file import.

Then: brainstorm the slice into a spec, turn the spec into a plan, execute it.
The two completed plans in `docs/superpowers/plans/` are the format that worked.

## Keeping the tracker honest

This is the part that decays if nobody owns it.

**When a feature ships** — move its row from ⬜ to ✅. If it shipped with a known
gap, mark it 🟡 **and name the gap**; a 🟡 with no explanation is worse than a ⬜
because it looks considered.

**When something is built that the design never described** — add a row. The
tracker maps what exists, not only what was drawn.

**When you find a design feature no row covers** — add it as ⬜. Finding a gap in
the list is work on the list.

**Recount the summary table.** Its columns must sum to the totals. Count the
status symbols per section rather than adjusting the numbers by hand — they were
wrong on the first attempt precisely because they were estimated.

**When something turns out not to be buildable** — say so where the row is, with
the reason. Bank sync is the worked example.

## Guardrails

- **Scope decisions belong to the user, not to you.** Surface the trade-off with
  a recommendation and let them choose. Several decisions in this product —
  whether a child can be granted Money access, whether the lockout is
  household-wide — went the way they did because they were asked.
- **Two features are out of scope by the design's own marking:** the kids' view
  and custom space pages, both labelled "· not built". Do not quietly add them.
- **Check `docs/LEARNING.md` before planning in an area with history.** Several
  defect classes here recurred because nobody checked whether the shape had
  appeared before.
- **A feature is not done because it works.** It is done when it works, meets the
  standards in `CLAUDE.md`, has a mutation-checked test, and the tracker and
  learning log are updated. That bar is stated in the tracker itself.

## Answering "how much is left"

Give the numbers from the tracker's summary table, and say plainly what they mean:
everything complete is entry, identity and household settings. The four
substantive areas of the product — the money, the marriage, the calendar and the
home screen that ties them together — have not been started. Being honest about
that is more useful than a percentage.
