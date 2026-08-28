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
} as const;
