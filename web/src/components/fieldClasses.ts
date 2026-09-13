// Class strings shared by form fields across every feature. Kept in a plain
// .ts file, not beside Field.tsx, because eslint's react-refresh rule refuses
// a non-component export from a component file.

// The text input / select every form modal uses, class for class -- it was
// written out by hand more than fifty times before it lived here.
//
// min-h-11 and sm:min-h-0 are not decoration. py-2.5 alone reaches only about
// 39px against this text's own line-height, 5px short of the 44px touch floor
// on a phone (TransactionFilters.tsx's SELECT_CLASS comment has the measured
// numbers). sm:min-h-0 hands the height back to the padding from the `sm`
// breakpoint up, where a pointer rather than a thumb is the likely input.
// Every modal used to repeat this explanation next to its first field; it
// lives here now, once, beside the classes it explains.
export const FIELD_CONTROL_CLASS =
  "min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] sm:min-h-0";
