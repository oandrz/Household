// The single definition of "N% used". It was written twice -- once in
// budgetCopy.ts and once in overview/copy.ts, both as `${percent}% used` --
// with three call sites between them, so fixing either one alone would have
// left the other standing. That is the sibling-defect shape docs/LEARNING.md
// records five times over.
//
// domain.PercentUsed rounds to the nearest whole percent, which is right: it
// is the *display* of a rounded-to-zero figure that lies. S$2.00 against
// S$800.00 is 0.25%, renders as 0, and reads as "nothing spent" to a household
// that has spent. spentMinor is what separates that from a month where
// genuinely nothing has been spent, and all three call sites already hold it
// -- so this needs no new field on the wire.
export function formatPercentUsed(percent: number, spentMinor: number): string {
  if (percent === 0 && spentMinor > 0) return "<1% used";
  return `${percent}% used`;
}
