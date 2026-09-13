// The Vision editor's local draft shapes, and the small pure helpers its
// editors share. A plain .ts file rather than part of a component file,
// because eslint's react-refresh rule refuses non-component exports there.
// Moved out of VisionModal.tsx with their comments unchanged.

export type DraftMeasure = {
  label: string;
  kind: "typed" | "linked";
  current: number;
  target: number;
  goalId: string;
};

export type DraftPillar = {
  name: string;
  description: string;
  measures: DraftMeasure[];
};

export type DraftMilestone = {
  year: number;
  title: string;
  note: string;
};

// A measure is typed OR linked, never both -- the domain refuses the
// ambiguous shape and so does the database's own measure_is_typed_or_linked.
// Switching modes therefore CLEARS the other mode's inputs rather than
// leaving them populated and hidden: a hidden value that still submits is
// how a form sends a body its own UI never showed anyone.
export function setMeasureMode(measure: DraftMeasure, mode: "typed" | "linked"): DraftMeasure {
  return mode === "typed"
    ? { ...measure, kind: "typed", goalId: "", current: 0, target: 1 }
    : { ...measure, kind: "linked", goalId: "", current: 0, target: 0 };
}

export function newMeasure(): DraftMeasure {
  return { label: "", kind: "typed", current: 0, target: 1, goalId: "" };
}

// Parses a bare, non-negative whole number typed into a plain text field --
// never NaN, which a raw `Number(event.target.value)` produces mid-edit (an
// empty field, a lone "-") and which would then sit in state as something
// `JSON.stringify` turns into `null` on the very next save. `type="text"
// inputMode="numeric"`, not `type="number"`, for the same reason every
// numeric field elsewhere in this codebase avoids it (formatMoney.ts's own
// convention) -- nothing here needs a spinner or the browser's own
// scientific-notation-accepting parser, and every value stays a plain JS
// number in state throughout, never a string re-parsed at save time.
export function parseWholeNumber(raw: string): number {
  const parsed = Number.parseInt(raw, 10);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
}
