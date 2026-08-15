// The outer box every page shares: a vertical stack with the design's own
// gutters at `sm` and above, and tighter ones below it -- 36px of padding
// either side costs 19% of a 375px screen, which is the difference between a
// readable ledger row and a wrapped one.
//
// Extracted rather than repeated because the class string below already
// existed verbatim at eight call sites. It forwards the rest of its props so
// pages can keep the `data-testid` their own tests query.
import type { HTMLAttributes, ReactNode } from "react";

export function PageContainer({
  children,
  className = "",
  ...rest
}: { children: ReactNode } & HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={`flex flex-col gap-5 px-4 py-6 sm:px-9 sm:py-8 ${className}`} {...rest}>
      {children}
    </div>
  );
}
