// The outer box every page shares: a vertical stack with the design's own
// gutters at `sm` and above, and tighter ones below it -- 36px of padding
// either side costs 19% of a 375px screen, which is the difference between a
// readable ledger row and a wrapped one.
//
// Extracted rather than repeated because the class string below already
// existed verbatim at eight call sites; a ninth, OverviewPage, carried
// `p-10` instead and was folded in here too -- its licensed 40 -> 32px
// change is now this shared class, not a separate override. It forwards the
// rest of its props so pages can keep the `data-testid` their own tests
// query.
import type { HTMLAttributes, ReactNode } from "react";

// Held in a constant rather than inlined in the template literal below: a
// Tailwind utility written immediately before `${` in a template literal is
// not extracted by the scanner, so the class is generated for every other
// occurrence in the codebase except this one -- it silently never exists.
// That is how `sm:py-8` shipped as a no-op here while `sm:px-9`, one token
// earlier on the same line, worked.
const BASE_CLASS = "flex flex-col gap-5 px-4 py-6 sm:px-9 sm:py-8";

export function PageContainer({
  children,
  className = "",
  ...rest
}: { children: ReactNode } & HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={className ? `${BASE_CLASS} ${className}` : BASE_CLASS} {...rest}>
      {children}
    </div>
  );
}
