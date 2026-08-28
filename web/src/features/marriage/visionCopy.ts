// Copy for the Vision & goals screen, in a plain .ts module for the same
// reason retroCopy.ts/goalCopy.ts/budgetCopy.ts/billCopy.ts are -- eslint's
// react-refresh/only-export-components rule never has to think about a file
// that mixes components with other exports, and every user-facing string
// lives in exactly one place.
//
// Not in task-11-brief.md's own file list (only VisionPage.tsx,
// PillarCard.tsx, MilestoneGrid.tsx and their tests, router.tsx, Sidebar.tsx
// are named) -- split out anyway, matching every one of its siblings above,
// none of which keep their copy inline in the page component either.
export const VISION_COPY = {
  title: "Vision & goals",
  subtitle: "Set every January, checked in at each retro",

  editVision: "Edit vision",

  // "2026 theme" -- the design's own small-caps label above the theme quote
  // (dc.html's is_vision screen). Mixed case in the DOM on purpose: the
  // visual uppercase comes from a CSS class (text-transform), not from
  // shouting in the string itself, so a screen reader announces "2026
  // theme" rather than a letter-by-letter caps run.
  themeLabel: (year: number) => `${year} theme`,
  // Wraps the household's own theme in literal quotation marks -- the
  // design's own `"Slow down together"` (dc.html) -- as real text, not a CSS
  // ::before/::after, so the mark is present in what a screen reader gets,
  // not only in what a sighted reader sees painted around it.
  themeQuote: (theme: string) => `"${theme}"`,

  pillarLabel: (n: number) => `Pillar ${n}`,

  // MeasureRow's own figureless branch (PillarCard.tsx). Covers three
  // server states that all mean the same thing to a reader: the number this
  // measure would have shown cannot be computed right now (measureDTO's own
  // comment in vision_handlers.go) -- a linked goal that was deleted, one
  // whose link failed to resolve, or a kind this build doesn't recognise.
  // One string for all three rather than a number that would state
  // something untrue about the household -- the same "blank the figure and
  // say why, never show a zero" rule Accounts applies when a
  // primary-currency change leaves net worth uncomputable.
  measureFigureUnavailable: "Goal removed",

  milestonesTitle: "Longer horizon",
  addMilestone: "+ Add milestone",

  // No vision exists yet for this year (visionSchema's own version: 0
  // comment) -- an invitation to set one, not a grid of cards with nothing
  // in them.
  emptyHeadline: (year: number) => `No vision set for ${year}`,
  emptyBody:
    "Set this year's theme, the pillars you're building it on and what you're both working toward.",
  emptyCta: "Set this year's vision",

  loadError: "Couldn't load this year's vision.",
  // GET /marriage/vision is marriage-AND-owner gated (router.go's own
  // comment on the group), the identical shape GET /retros carries
  // (retroCopy.ts's own ownerOnlyHeading/Body) -- a limited member who
  // reaches this route (RequireCapability only checks the capability, not
  // the role) is told plainly why, not shown the same red alert a genuine
  // server failure gets. This is the exact gap BillsPage.tsx shipped without
  // (docs/LEARNING.md pattern 1), found again in BudgetPage.tsx and
  // TransactionsPage.tsx -- GoalsPage.tsx/RetrosPage.tsx already carry the
  // fix, restated here.
  ownerOnlyHeading: "Owner only",
  ownerOnlyBody:
    "Vision & goals is visible to the household owner. Ask them if you'd like to see where things stand.",

  // --- VisionModal (Task 12) ---------------------------------------------

  // dc.html's own modalVision header: title plus the privacy/cadence note
  // shown under it, verbatim.
  modalTitle: "Edit our vision",
  modalPrivacyBadge: "🔒 Private — parents only. Usually set together each January.",

  modalThemeLabel: "This year's theme",
  modalYearLabel: "Year",
  modalDescriptionLabel: "What it means to us",

  modalPillarsHeading: "Pillars",
  addPillar: "+ Add pillar",
  modalPillarNameLabel: "Pillar name",
  modalPillarDescriptionLabel: "What this pillar means",
  // A pillar's own name, when it has one, makes the control specific enough
  // that a screen reader announcing every "Remove" button on the page (this
  // modal can hold up to 12) doesn't read the same word twelve times running
  // -- the identical reason GoalContributionsPanel.tsx names its own
  // per-row delete controls. Falls back to a plain "Remove pillar" for a
  // freshly-added one that has no name yet.
  removePillar: (name: string) => (name ? `Remove pillar "${name}"` : "Remove pillar"),

  modalMeasuresHeading: "Measures",
  addMeasure: "+ Add measure",
  modalMeasureLabelLabel: "Label",
  modalMeasureModeLabel: "Track by",
  // The two choices decision 1 draws a hard line between: a plain number the
  // household types in themselves, or this measure's progress read live off
  // a savings goal. Never both -- see setMeasureMode's own comment, the one
  // place that rule is enforced.
  modalMeasureModeTyped: "A number we keep",
  modalMeasureModeLinked: "A savings goal",
  modalMeasureCurrentLabel: "Current",
  modalMeasureTargetLabel: "Target",
  modalMeasureGoalLabel: "Goal",
  modalMeasureGoalPlaceholder: "Choose a goal",
  removeMeasure: (label: string) => (label ? `Remove measure "${label}"` : "Remove measure"),

  modalMilestoneYearLabel: "Year",
  modalMilestoneTitleLabel: "Title",
  modalMilestoneNoteLabel: "Note",
  removeMilestone: (title: string) => (title ? `Remove milestone "${title}"` : "Remove milestone"),

  cancel: "Cancel",
  saveVision: "Save vision",

  // The two client-side checks this modal makes before ever reaching the
  // server -- see handleSave's own comments in VisionModal.tsx for why
  // each exists. The first (advisor's own finding: the empty state seeds
  // theme: "", so a brand-new household's very first Save would otherwise
  // round-trip a 422 for something checkable in three lines) -- Hearth's
  // own message, not the browser's, per the UI-polish round (CLAUDE.md's
  // own rule).
  modalThemeRequired: "Give this year a theme before saving.",
  // The second: a measure switched to "A savings goal" and saved without
  // one picked is neither typed nor linked. Deliberately does not echo
  // VISION_MEASURE_INVALID's server-side wording ("not both") -- that
  // message is wrong here, since this household picked neither, and
  // errors.go's own ErrVisionMeasureGoalRequired case carries the matching
  // server-side copy for the same reason.
  modalMeasureGoalRequired: "Pick a savings goal for that measure, or switch it back to a number you keep.",
  modalSaveError: "Couldn't save this year's vision. Try again.",

  // The version-conflict banner. Named for what the button actually does --
  // discards the draft and shows the partner's version -- rather than a bare
  // "Reload", which would read as a safe, resumable refresh. See
  // VisionModal.tsx's own header comment for why that's the only honest
  // action to offer here.
  conflictBanner:
    "Someone else saved this year's vision while you were editing. Nothing has been sent -- but reloading will discard the changes you made here and show their version instead.",
  reloadAndDiscardChanges: "Reload and discard my changes",
} as const;
