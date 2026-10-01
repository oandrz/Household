// The Hearth mark: a white "H" on the accent square. One component, because
// the square was written out by hand at nine places and drifted into two
// sizes with no glyph in any of them.
//
// aria-hidden: the wordmark "Hearth" always sits beside it, and a mark read
// aloud would make that "H Hearth".
//
// Two sizes, both the design's own: the app shell's 28px square and the
// sign-in screens' 30px one. Whole class strings rather than a computed size,
// because Tailwind only emits a utility it can read in the source as a literal.
const SIZE_CLASS = {
  shell: "h-7 w-7 rounded-lg text-[15px]",
  auth: "h-[30px] w-[30px] rounded-[9px] text-[16px]",
} as const;

export function BrandMark({ size }: { size: keyof typeof SIZE_CLASS }) {
  return (
    <div
      aria-hidden="true"
      className={`grid flex-none place-items-center bg-accent font-bold leading-none text-white ${SIZE_CLASS[size]}`}
    >
      H
    </div>
  );
}
