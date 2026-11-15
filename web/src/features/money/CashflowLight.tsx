// The traffic light: a coloured dot, its name in words, and each reason. The
// dot is decoration; the words carry the meaning, so it reads the same to a
// screen reader and to someone who cannot tell green from red.
import { LIGHT_LABEL, reasonText } from "./cashflowCopy";
import type { CashflowPage } from "./cashflowSchemas";

const DOT_CLASS = {
  green: "bg-accent",
  yellow: "bg-warn",
  red: "bg-danger",
  grey: "bg-muted",
} as const;

const TEXT_CLASS = {
  green: "text-accent",
  yellow: "text-warn",
  red: "text-danger",
  grey: "text-muted",
} as const;

type Props = {
  light: CashflowPage["light"];
  averageMonths: number;
};

export function CashflowLight({ light, averageMonths }: Props) {
  return (
    <section
      data-testid="cashflow-light"
      data-colour={light.colour}
      className="rounded-xl border border-hairline bg-card p-[22px]"
    >
      <div className="flex items-center gap-2.5">
        <span aria-hidden="true" className={`inline-block h-3.5 w-3.5 rounded-full ${DOT_CLASS[light.colour]}`} />
        <h2 className={`text-[19px] font-semibold tracking-[-0.01em] ${TEXT_CLASS[light.colour]}`}>
          {LIGHT_LABEL[light.colour]}
        </h2>
      </div>
      {light.reasons.length > 0 && (
        <ul className="mt-3 flex flex-col gap-1.5 text-[13.5px] leading-relaxed text-ink">
          {light.reasons.map((reason, index) => (
            <li key={`${reason.code}-${index}`}>{reasonText(reason, averageMonths)}</li>
          ))}
        </ul>
      )}
    </section>
  );
}
