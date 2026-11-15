// One labelled number with optional small notes beneath it. Shared by the
// Monthly and Yearly views so both set a figure out the same way.
import type { ReactNode } from "react";

type Props = {
  label: string;
  value: string;
  testId?: string;
  children?: ReactNode;
};

export function CashflowFigure({ label, value, testId, children }: Props) {
  return (
    <div data-testid={testId} className="rounded-xl border border-hairline bg-card p-[22px]">
      <div className="text-xs text-muted">{label}</div>
      <div className="mt-1 text-[23px] font-semibold tracking-[-0.02em] text-ink">{value}</div>
      {children && <div className="mt-1.5 flex flex-col gap-0.5 text-xs leading-snug text-muted">{children}</div>}
    </div>
  );
}
