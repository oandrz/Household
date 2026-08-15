// What a household still has to do, on the page they land on. Renders nothing
// once every step is done, so an established household is not shown a
// permanent chore list.
//
// Takes its state as props rather than fetching: both real steps read data
// OverviewPage already holds, and a second fetch would both double the
// requests on the most-visited page and let this list disagree with the cards
// beside it about the same numbers.
//
// There is deliberately no "invite your partner" step, though the design's
// own onboarding implies one. An emailed invite writes only to the `invites`
// table -- InviteService.Create -- while GET /household/members reads
// memberships joined to users, so a pending invite is not a row there and no
// endpoint exposes one. The step could therefore only tick when the partner
// *accepted*, leaving an owner who had just invited someone looking at an
// unticked "Invite your partner" whose link leads to a Settings page showing
// no trace of the invite they sent. That is the same failure Budget's spec
// refused when it cut the dormant "Roll unspent into savings" toggle, and
// the rule Sidebar.tsx states. The step joins this list in the change that
// exposes pending invites.
import { Link } from "@tanstack/react-router";
import { OVERVIEW_COPY } from "./copy";

// Read at render time -- a household that opens the app in August must not be
// told to budget for July.
function monthName(): string {
  return new Date().toLocaleString(undefined, { month: "long" });
}

export function SetupChecklist({
  hasAccount,
  hasBudget,
}: {
  hasAccount: boolean;
  hasBudget: boolean;
}) {
  const steps = [
    // Always done: reaching this page at all required creating one. It is
    // listed anyway so the first thing a new household sees is something
    // already achieved rather than three things outstanding.
    { label: OVERVIEW_COPY.setupHousehold, done: true, to: null },
    { label: OVERVIEW_COPY.setupAccount, done: hasAccount, to: "/money" as const },
    { label: OVERVIEW_COPY.setupBudget(monthName()), done: hasBudget, to: "/money/budget" as const },
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
            {/* inline-flex items-center min-h-11 sm:min-h-0:
                BudgetCard.tsx's own comment on this identical pattern has
                the reason. */}
            {!step.done && step.to && (
              <Link
                to={step.to}
                className="inline-flex min-h-11 items-center text-[12.5px] font-semibold text-accent sm:min-h-0"
              >
                {OVERVIEW_COPY.setupGo}
              </Link>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
