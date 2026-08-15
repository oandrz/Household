// Two form fields side by side, stacking below `sm`. Inside a modal panel
// that measures 343px on a 375px phone, two columns leave each field about
// 155px -- narrower than the date and amount inputs they usually hold.
//
// Extracted because this exact grid already appeared at thirteen call sites
// across eight modals; a fourteenth would have been a fourteenth place to
// forget the breakpoint.
import type { ReactNode } from "react";

export function FieldPair({ children }: { children: ReactNode }) {
  return <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">{children}</div>;
}
