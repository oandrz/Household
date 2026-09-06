# GitHub issue draft — Retros/Vision stale-version overwrite

Not filed. Filing on the user's own repository is theirs to authorise, not
mine to do on their behalf (`docs/agents/issue-tracker.md`). This file is the
ready-to-paste body; run:

```
gh issue create --title "Retros/Vision: a still-open modal can silently overwrite a partner's concurrent save" \
  --body-file docs/superpowers/plans/2026-09-06-retro-vision-stale-version-issue.md \
  --label "needs-product-decision"
```

(swap in whatever label this repo actually uses for a product-decision-needed
item — `docs/agents/triage-labels.md` has the vocabulary; nothing here should
be read as having already decided one).

---

## Title

Retros/Vision: a still-open modal can silently overwrite a partner's concurrent save

## The question this issue exists to answer

**When a second household member's write lands while someone's Retro or
Vision edit modal is still open, and that person then clicks Save, what
should the app do?**

Today the answer is: nothing tells them, and the later Save wins outright —
`200 OK`, no banner, no trace of the write it erased. That is not the
product's chosen answer to concurrent edits; it is a gap in *reaching* the
answer the product already gives everywhere else. See "What is already
solved" below before treating this as a bigger question than it is.

## Where this lives in the code

- `web/src/features/marriage/useRetro.ts:144-161` — `saveMutation`'s
  `mutationFn` reads `const current = query.data` fresh at send time and
  attaches `current.retro.version` to the `PATCH`.
- `web/src/features/marriage/useVision.ts:101-113` — the identical shape:
  `saveMutation`'s `mutationFn` reads `query.data` fresh and attaches
  `current.version` to the `PUT`.
- `web/src/features/marriage/RetroModal.tsx:140-148` — the modal's own
  `mood`/`wentWell`/`wasHard`/`notes` draft is seeded from `retro.data`
  exactly once (`!initialized`) and never re-seeded from a later
  `query.data`. `VisionModal.tsx` seeds its own draft the same way, once.
- `web/src/main.tsx:11` — `staleTime: 30_000`, and TanStack Query v5 (this
  project runs `5.101.4`) defaults `refetchOnWindowFocus` to `true`, which
  nothing here turns off. So the query backing an open modal can refetch
  on its own — not only via `RetroModal`'s in-modal `addAction` calls
  (lines 242 and 281) — any time the tab regains focus more than thirty
  seconds after the modal opened.

Put together: a refetch lands (window focus, or Retros' own `addAction`),
`query.data.retro.version` (or `.version` for Vision) moves to whatever a
partner's own concurrent write already committed, and the still-open
modal's draft — seeded once, untouched by the refetch — keeps its original
text. The next Save reads the *new* version live and attaches it to the
*old* draft. The server sees a current version, accepts it, and the
partner's write is gone with nothing on either screen to say so.

## What is already solved — and why the fix is smaller than it looks

The product already has an answer for two members editing the same
document: **`RETRO_CHANGED`/`VISION_CHANGED` plus a one-way `hadConflict`
latch.** When a save's *own* version is already stale at send time, the
server refuses it, the modal shows a conflict banner, and — same shape as
Agreements' decision 13 — nothing typed is lost.

The gap is not that this answer is wrong. The gap is that it never
**fires**, because the version driving conflict detection is read live off
`query.data` at send time instead of being fixed at the moment the modal
opened. A version that can silently update itself between "I started
editing" and "I clicked Save" cannot detect staleness — it just agrees
with whatever is current. **Snapshotting the version into its own
`useState` at modal-open, and reading that snapshot rather than the live
prop, is what makes the 409 fire on the very first stale Save instead of
never.** This is the identical shape to this branch's own fix in
`1325197`: `ProposeAgreementModal.tsx` had the same live-prop-read bug for
`previousBody`, and the fix was exactly this — snapshot once, at the
moment the value is chosen, read the snapshot at send time.

That means the core of this issue is a known, already-precedented fix, not
new design. It does not need "its own product decision" in the sense of
inventing new UX — the conflict banner, the one-way latch, and the copy
all already exist and are already correct for the case they do reach.

## The one genuinely open edge

Snapshotting the version at modal-open closes the *first* stale Save. It
does not, by itself, say what should happen on a **second** Save from a
still-open modal, made *after* the first Save already succeeded in that
same session:

- **Refresh the snapshot from the write's own response.** The first
  Save's `200 OK` response carries the new version the server just
  produced; if the snapshot updates to that value, a second Save in the
  same sitting is checked against the version *this tab itself just
  wrote*, and only a *third party's* intervening write would trip the
  conflict — which is correct, and is what "stale" ought to mean.
- **Leave the snapshot as originally taken.** A second Save would then
  compare against the version from when the modal opened, which is now
  provably stale (this tab's own first Save moved it) — so it would
  either need special-casing ("my own write doesn't count as stale") or
  it would false-positive a conflict against nobody, on your own edit.

This needs a decision, not because the mechanism is unclear, but because
it is a genuine two-way trade-off between "trust your own most recent
write" and "always re-verify against the server before writing again" —
and only the product owner's judgement about how these modals are meant to
be used (open-and-done, versus open-and-iterate-with-repeated-Saves) can
settle which one is right.

## Scope note

Filed off Agreements' whole-branch review, which found this while sweeping
for siblings of `1325197`'s own live-read defect. It is pre-existing code
in two already-shipped features (Retros, Vision) and is intentionally not
being fixed on the `agreements-spec` branch — see `docs/LEARNING.md`
pattern 18 and pattern 1's Retros/Task 13 bullet for the fuller mechanism,
and `docs/FEATURE_TRACKER.md`'s `Start retro (modal)` and `Edit vision
(modal)` rows for the tracker's own record of the gap.
