// The interface's icons, drawn rather than typed.
//
// These were Unicode characters until 2026-09-01 -- "⏻" (U+23FB POWER
// SYMBOL) for sign out, "☰" (U+2630) for the mobile nav, "✕" (U+2715) for
// every close and remove control. A character only appears if some font on
// the device covers that codepoint, and the product owner's Samsung Fold 7
// covers none of U+23FB: the sign-out control rendered as an empty button in
// the navigation drawer. It looked correct on macOS, where the system font
// happens to fill the gap, which is why it survived every desktop review.
// Schibsted Grotesk (this app's own webfont, see index.css) carries none of
// these three either -- every one of them was relying on whatever the device
// fell back to.
//
// An SVG carries its own shape, so it renders identically on a phone with no
// symbol font at all. `currentColor` keeps each one inheriting the colour of
// the control holding it, exactly as the character did, and the caller passes
// its own size class because these sit in controls that shrink at `sm`/`lg`.
//
// Each icon is decorative: every control using one already carries its
// meaning in an `aria-label`, so `aria-hidden` keeps a screen reader from
// announcing the drawing as a second, nameless thing.

type IconProps = {
  /** Tailwind size classes, e.g. "h-4 w-4". Colour comes from the parent. */
  className?: string;
};

// The IEC 60417-5009 power symbol: a broken ring with a stem through the gap.
export function PowerIcon({ className = "h-4 w-4" }: IconProps) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d="M12 3.5v8" />
      <path d="M7.8 6.3a7.5 7.5 0 1 0 8.4 0" />
    </svg>
  );
}

export function MenuIcon({ className = "h-[18px] w-[18px]" }: IconProps) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d="M4 7h16" />
      <path d="M4 12h16" />
      <path d="M4 17h16" />
    </svg>
  );
}

export function CloseIcon({ className = "h-3.5 w-3.5" }: IconProps) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d="M6 6l12 12" />
      <path d="M18 6L6 18" />
    </svg>
  );
}
