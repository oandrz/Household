// One section of the agreements document: its name, its live count and its
// numbered rows. A pure presentation component -- it takes the section the
// page already fetched, so nothing here reaches the network.
import { AGREEMENT_COPY } from "./agreementCopy";
import type { AgreementSection } from "./agreementSchemas";

export function AgreementSectionCard({ section }: { section: AgreementSection }) {
  return (
    <div
      data-testid={`agreement-section-${section.id}`}
      className="rounded-xl border border-hairline bg-card p-[22px]"
    >
      <div className="mb-3.5 flex items-baseline justify-between gap-3">
        {/* h3 under the page's h1: those are the only two heading levels on
            this screen, so a screen reader's outline matches what is drawn. */}
        <h3 className="text-sm font-semibold text-ink">{section.name}</h3>
        {/* shrink-0 + whitespace-nowrap for PillarCard's own reason: a long
            section name crowds this span for width, and without them the
            count wraps mid-phrase ("3" / "agreements"). */}
        <span className="shrink-0 whitespace-nowrap text-[11px] text-muted">
          {AGREEMENT_COPY.sectionCount(section.count)}
        </span>
      </div>
      <div className="flex flex-col gap-3 text-[13px] leading-[1.55] text-label">
        {section.agreements.map((agreement) => (
          <div key={agreement.id} data-testid={`agreement-row-${agreement.id}`} className="flex gap-2.5">
            {/* The service composes this integer for the whole document on
                every read and stores it nowhere (decisions 10 and 11), so this
                card pads and derives nothing -- which is why a section
                legitimately starts at 03. Deriving a number here would restart
                every section at 01 and break the design's continuous run. */}
            <span className="flex-none font-semibold text-accent">
              {String(agreement.number).padStart(2, "0")}
            </span>
            <span>{agreement.body}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
