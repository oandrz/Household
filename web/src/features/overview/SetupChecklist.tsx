// What a household still has to do, on the page they land on. Renders nothing
// once every step is done, so an established household is not shown a
// permanent chore list.
//
// Takes its state as props rather than fetching: both real steps read data
// OverviewPage already holds, and a second fetch would both double the
// requests on the most-visited page and let this list disagree with the cards
// beside it about the same numbers.
//
// "Invite your partner" reads the roster and the pending invites
// (partnerStep.ts). It could not exist until GET /household/invites did: an
// invite was never read back, so the step could only tick when the partner
// *accepted*, leaving an owner who had just invited someone looking at an
// unticked step whose link showed no trace of the invite they sent.
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { OVERVIEW_COPY } from "./copy";
import type { PartnerStep } from "./partnerStep";

// Read at render time -- a household that opens the app in August must not be
// told to budget for July.
function monthName(): string {
  return new Date().toLocaleString(undefined, { month: "long" });
}

// inline-flex items-center min-h-11 sm:min-h-0: BudgetCard.tsx's own comment
// on this identical pattern has the reason.
const GO_LINK = "inline-flex min-h-11 items-center text-[12.5px] font-semibold text-accent sm:min-h-0";

export function SetupChecklist({
  hasAccount,
  hasBudget,
  partner,
}: {
  hasAccount: boolean;
  hasBudget: boolean;
  partner: PartnerStep;
}) {
  const steps: { label: string; done: boolean; link: ReactNode }[] = [
    // Always done: reaching this page at all required creating one. It is
    // listed anyway so the first thing a new household sees is something
    // already achieved rather than everything outstanding.
    { label: OVERVIEW_COPY.setupHousehold, done: true, link: null },
    {
      label: OVERVIEW_COPY.setupAccount,
      done: hasAccount,
      link: (
        <Link to="/money" className={GO_LINK}>
          {OVERVIEW_COPY.setupGo}
        </Link>
      ),
    },
    {
      label: OVERVIEW_COPY.setupBudget(monthName()),
      done: hasBudget,
      link: (
        <Link to="/money/budget" className={GO_LINK}>
          {OVERVIEW_COPY.setupGo}
        </Link>
      ),
    },
    {
      label: partner === "invited" ? OVERVIEW_COPY.setupPartnerInvited : OVERVIEW_COPY.setupPartner,
      // "Invited" is not done: the step finishes when the partner is in, not
      // when the invite leaves.
      done: partner === "joined",
      // Once an invite is out, the link shows it rather than opening a second
      // invite modal over it.
      link:
        partner === "invited" ? (
          <Link to="/settings" className={GO_LINK}>
            {OVERVIEW_COPY.setupPartnerSee}
          </Link>
        ) : (
          <Link to="/settings" search={{ invite: true }} className={GO_LINK}>
            {OVERVIEW_COPY.setupGo}
          </Link>
        ),
    },
  ];

  const done = steps.filter((s) => s.done).length;
  if (done === steps.length) return null;

  return (
    <section
      aria-labelledby="overview-setup-heading"
      className="flex flex-col rounded-xl border border-hairline bg-card p-[22px]"
    >
      <div className="flex items-baseline justify-between">
        <h2 id="overview-setup-heading" className="text-sm font-semibold text-ink">
          {OVERVIEW_COPY.setupHeading}
        </h2>
        <span className="text-[11.5px] text-muted">
          {OVERVIEW_COPY.setupProgress(done, steps.length)}
        </span>
      </div>

      <ul className="mt-3 flex flex-col gap-2.5">
        {steps.map((step) => (
          <li key={step.label} className="flex items-center justify-between text-[13px]">
            <span className={step.done ? "text-muted line-through" : "text-ink"}>
              {step.done ? "✓ " : ""}
              {step.label}
            </span>
            {!step.done && step.link}
          </li>
        ))}
      </ul>
    </section>
  );
}
