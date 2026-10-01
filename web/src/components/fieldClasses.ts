// Class strings shared by form fields across every feature. Kept in a plain
// .ts file, not beside Field.tsx, because eslint's react-refresh rule refuses
// a non-component export from a component file.

// The text input / select every form modal uses, class for class -- it was
// written out by hand more than fifty times before it lived here.
//
// min-h-11 and lg:min-h-0 are not decoration. py-2.5 alone reaches only about
// 39px against this text's own line-height, 5px short of the 44px touch floor
// on a phone (TransactionFilters.tsx's SELECT_CLASS comment has the measured
// numbers). lg:min-h-0 hands the height back to the padding from the `lg`
// breakpoint (1024px) up, where a pointer rather than a thumb is the likely
// input.
//
// `lg`, not `sm`: this is the one rule for every control in the app. 640 to
// 1023px is tablet width, held and tapped, and the shell still shows its
// phone-style drawer there. Layout may change at `sm` (a field going from
// full width to its natural width); the touch floor must not, so one class
// string can carry `sm:w-auto` and `lg:min-h-0` side by side.
// Every modal used to repeat this explanation next to its first field; it
// lives here now, once, beside the classes it explains.
export const FIELD_CONTROL_CLASS =
  "min-h-11 rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px] lg:min-h-0";

// On the <label> that wraps a ToggleSwitch and its words. The wrapper is a
// label so that clicking the words flips the switch, the way every native
// checkbox behaves; these two classes make the pointer say so, and say "not
// now" while the switch inside is disabled.
export const TOGGLE_LABEL_CLASS = "cursor-pointer has-[:disabled]:cursor-not-allowed";
