// Renders from `me.spaces`, never a hard-coded list -- that is the property
// that lets "+ New space" (Task 20) extend the navigation without a change
// here. The server has already filtered this array by the caller's
// capabilities and ordered it by position (domain.VisibleSpaces); this
// component renders it exactly as given, in that order, and does not
// re-sort or re-filter it.
//
// The design's 5a sidebar groups each space into an uppercase label plus
// several sub-page links (Finances/Transactions/Budget/... under Money, and
// so on). That grouped form arrived with Transactions: Money now has two
// built pages, so it renders as a label plus two links. SPACE_PAGES grows a
// row per shipped page -- Budget, Goals and Bills join it once their pages
// exist, not before, because a permanent grey "soon" row reads as broken.
import { Link, useMatchRoute, useNavigate } from "@tanstack/react-router";
import type { Me, Space } from "../auth/schemas";
import { useSignOut } from "../auth/useAuth";

// One entry per built page of each space, in the design's order. A space
// with one entry renders as a single link named after the space; a space
// with several renders as the design's uppercase group label plus a link
// per page (the 5a sidebar). A space key this map doesn't recognise --
// reachable once "+ New space" lets a household create one -- renders as
// plain text rather than a Link to a route that doesn't exist.
// `label` is only ever read for a space with 2+ pages (the group-label form
// below) -- a single-page space renders `space.name`, the household's own
// name for that space, instead. Marriage and Family both carry a `label`
// here purely to keep this map's shape uniform; setting it to anything else
// would never show up. Don't "fix" a single-page space's label expecting it
// to render -- see SpaceLink's single-link branch.
const SPACE_PAGES: Record<string, { label: string; to: string }[]> = {
  money: [
    { label: "Finances", to: "/money" },
    { label: "Transactions", to: "/money/transactions" },
  ],
  marriage: [{ label: "Marriage", to: "/marriage" }],
  family: [{ label: "Family", to: "/family/calendar" }],
};

// Layout only -- no color here. `text-ink`/`text-accent`/`text-muted` are
// added by each caller instead of baked in, because Tailwind's cascade picks
// whichever color utility's generated rule sits later in the stylesheet,
// not whichever appears later in the `className` string. A link that always
// carries both `text-ink` (from a shared base) and a conditional
// `text-accent` renders ink even while active, no matter which one comes
// last in the markup -- that shipped as a defect here (grouped Money links
// never showed the accent). Give each link exactly one color class, chosen
// by the caller, so there is nothing for the cascade to arbitrate.
const NAV_ITEM_CLASS = "rounded-lg px-2.5 py-2 text-[13.5px] font-semibold";

function SpaceLink({ space }: { space: Space }) {
  const matchRoute = useMatchRoute();
  const pages = SPACE_PAGES[space.key];
  if (!pages) {
    return (
      <span data-testid="sidebar-space" className={`${NAV_ITEM_CLASS} text-muted`}>
        {space.name}
      </span>
    );
  }
  if (pages.length === 1) {
    // Single-page spaces (Marriage, Family) get the same route-driven accent
    // as a grouped page below -- `matchRoute` defaults to an exact match
    // (fuzzy: false), which is deliberate here too: Family's only route
    // today is /family/calendar, so exact and prefix behave identically for
    // it right now, but exact is the correct choice going forward -- it is
    // what stops a later sub-page under /family from also lighting up this
    // link, the same reason the grouped Money links need it against
    // /money/transactions.
    const isActive = Boolean(matchRoute({ to: pages[0].to }));
    return (
      <Link
        data-testid="sidebar-space"
        to={pages[0].to}
        className={`${NAV_ITEM_CLASS} ${isActive ? "text-accent" : "text-ink"}`}
      >
        {space.name}
      </Link>
    );
  }
  return (
    <>
      {/* The group label, not a link -- kept under its own testid so a test
          asserting link order isn't also asserting where this label sits in
          a flattened list of unrelated node kinds. */}
      <div
        data-testid="sidebar-space-label"
        className="px-2.5 pb-1 pt-3.5 text-[10.5px] font-semibold uppercase tracking-[0.09em] text-muted"
      >
        {space.name}
      </div>
      {pages.map((page) => {
        // Computed here, not via Link's activeProps -- activeProps merges
        // its className with the base className rather than replacing it,
        // so the base's color class and the active color class would both
        // be present at once (see the NAV_ITEM_CLASS comment). Doing the
        // match ourselves means the Link only ever carries one color class.
        // `matchRoute` defaults to an exact match (fuzzy: false), so
        // "/money" only matches "/money" itself, never "/money/transactions"
        // -- the same distinction Link's `activeOptions.exact` makes, kept
        // here without needing that option.
        const isActive = Boolean(matchRoute({ to: page.to }));
        return (
          <Link
            key={page.to}
            data-testid="sidebar-space"
            to={page.to}
            className={`${NAV_ITEM_CLASS} ${isActive ? "text-accent" : "text-ink"}`}
          >
            {page.label}
          </Link>
        );
      })}
    </>
  );
}

export function Sidebar({ me }: { me: Me }) {
  const signOut = useSignOut();
  const navigate = useNavigate();
  const matchRoute = useMatchRoute();
  // Overview and Settings accent on their own route the same way each
  // SpaceLink does -- before this, Overview was unconditionally text-accent
  // and Settings unconditionally text-ink, so visiting /settings still left
  // Overview looking active and Settings never lit up at all. Settings is a
  // single route (SettingsPage renders Members/Spaces/Currency as tabs
  // inside it, not separate routes -- see routes/router.tsx), so exact match
  // is unambiguous here.
  const overviewActive = Boolean(matchRoute({ to: "/" }));
  const settingsActive = Boolean(matchRoute({ to: "/settings" }));

  function handleSignOut() {
    signOut.mutate(undefined, {
      onSuccess: () => navigate({ to: "/sign-in" }),
    });
  }

  return (
    <nav className="flex flex-col gap-0.5 overflow-y-auto border-r border-hairline bg-card px-4 py-[22px]">
      {/* The design's sidebar has no separate top bar; this brand row (logo
          square, "Hearth", the ⌘K chip) is the only header it draws, so
          AppShell's "header" is this. The ⌘K chip is static -- no command
          palette exists to open. */}
      <div className="flex items-center gap-2.5 px-2.5 pb-[18px]">
        <div className="h-7 w-7 rounded-lg bg-accent" />
        <div className="text-[15px] font-semibold tracking-[-0.01em]">
          Hearth
        </div>
        <div className="ml-auto rounded-md border border-hairline px-1.5 py-0.5 text-[11px] text-muted">
          ⌘K
        </div>
      </div>

      <Link to="/" className={`${NAV_ITEM_CLASS} ${overviewActive ? "text-accent" : "text-ink"}`}>
        Overview
      </Link>

      {me.spaces.map((space) => (
        <SpaceLink key={space.id} space={space} />
      ))}

      <div className="flex-1" />

      <Link to="/settings" className={`${NAV_ITEM_CLASS} ${settingsActive ? "text-accent" : "text-ink"}`}>
        Settings
      </Link>

      <div className="mt-2 flex items-center gap-[7px] border-t border-hairline pt-3">
        <div className="grid h-7 w-7 flex-none place-items-center rounded-full bg-accent text-[11px] font-semibold text-white">
          {me.user.avatarInitial}
        </div>
        <div className="min-w-0 flex-1 truncate text-xs leading-tight text-label">
          {me.household.name}
          <br />
          {/* Static per the design; billing/plans are out of scope for this
              slice (docs/superpowers/specs/2026-07-26-hearth-foundation-design.md). */}
          <span className="text-[11px] text-muted">Free plan</span>
        </div>
        <button
          type="button"
          title="Sign out"
          aria-label="Sign out"
          onClick={handleSignOut}
          disabled={signOut.isPending}
          className="grid h-6 w-6 flex-none place-items-center rounded-md text-[13px] text-label disabled:opacity-60"
        >
          ⏻
        </button>
      </div>
    </nav>
  );
}
