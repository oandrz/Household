// One labelled form field: the label, the control under it, and -- when there
// is one -- that field's own error line under the control.
//
// Extracted because this exact wrapper and label (`flex flex-col gap-1.5`
// around a `text-xs font-semibold text-label` <label>) had been copied by hand
// at more than fifty call sites across nineteen files, past the bar
// FieldPair.tsx set for sharing a piece of markup.
//
// What it deliberately does NOT cover, so those places still write their own
// markup:
// - a label that is not a <label htmlFor> (a <span> over a read-only value, or
//   a <label> that wraps its input, as the Holding panels do -- that shape
//   labels its input implicitly and has no id to point htmlFor at);
// - a wrapper with extra layout classes (`min-w-0 flex-1`, `justify-end`) or a
//   smaller label size (VisionModal's measure fields use text-[11px]);
// - a label row aligned on the text baseline (the sign-in and sign-up password
//   fields), which `labelAside` below does not reproduce.
//
// Hint text is passed inside `children`, not as a prop: the hints in this app
// use three slightly different text sizes and line heights, and a prop would
// have forced them into one.
import type { ReactNode } from "react";

// RetroModal colour-codes its two main headings -- "What went well" in the
// accent colour, "What was hard" in danger. Every other field uses the
// default.
const LABEL_TONE_CLASS = {
  default: "text-label",
  accent: "text-accent",
  danger: "text-danger",
} as const;

export function Field({
  label,
  htmlFor,
  children,
  error,
  errorTestId,
  labelTone = "default",
  labelAside,
}: {
  label: ReactNode;
  // The id of the control inside `children`. Required: a label that is not
  // tied to its control is invisible to a screen reader and to every
  // getByLabelText in the test suite.
  htmlFor: string;
  // The control itself, plus anything that belongs directly under it (a hint).
  children: ReactNode;
  // Rendered as a role="alert" line after `children` whenever it is a
  // non-empty string.
  error?: string | null;
  errorTestId?: string;
  labelTone?: keyof typeof LABEL_TONE_CLASS;
  // A small control drawn on the label's own row, pushed to the far end -- a
  // toggle (GoalModal's "No target date") or a link (ProposeAgreementModal's
  // "New section").
  labelAside?: ReactNode;
}) {
  const labelElement = (
    <label htmlFor={htmlFor} className={`text-xs font-semibold ${LABEL_TONE_CLASS[labelTone]}`}>
      {label}
    </label>
  );

  return (
    <div className="flex flex-col gap-1.5">
      {labelAside ? (
        <div className="flex items-center justify-between gap-3">
          {labelElement}
          {labelAside}
        </div>
      ) : (
        labelElement
      )}
      {children}
      {error && (
        <p role="alert" data-testid={errorTestId} className="text-xs leading-snug text-danger">
          {error}
        </p>
      )}
    </div>
  );
}
