// Two form fields side by side, stacking below `sm`. Inside a modal panel
// that measures 343px on a 375px phone, two columns leave each field about
// 155px -- narrower than the date and amount inputs they usually hold.
//
// Extracted because this exact grid already appeared at twelve call sites
// across seven files; a thirteenth would have been a thirteenth place to
// forget the breakpoint. BudgetStatCards.tsx:60 carries an extra md:grid-cols-4
// and was correctly left alone.
import type { ReactNode } from "react";

export function FieldPair({ children }: { children: ReactNode }) {
  return <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">{children}</div>;
}
