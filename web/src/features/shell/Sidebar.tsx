// Renders from `me.spaces`, never a hard-coded list -- that is the property
// that lets "+ New space" (Task 20) extend the navigation without a change
// here. The server has already filtered this array by the caller's
// capabilities and ordered it by position (domain.VisibleSpaces); this
// component renders it exactly as given, in that order, and does not
// re-sort or re-filter it.
//
// The design's 5a sidebar groups each space into an uppercase label plus
// several sub-page links (Finances/Transactions/Budget/... under Money, and
// so on). That grouped form arrived with Transactions and now carries
// Budget, Goals and Bills too: Money renders as a label plus five links.
// SPACE_PAGES grows a row per shipped page -- a page joins it once its route
// exists, not before, because a permanent grey "soon" row reads as broken.
import { Link, useMatchRoute, useNavigate } from "@tanstack/react-router";
import type { Me, Space } from "../auth/schemas";
import { useSignOut } from "../auth/useAuth";

// One entry per built page of each space, in the design's order. A space
// present here renders as the design's uppercase group label plus a link
// per page (the 5a sidebar) -- including a space with only one page. No
// entry has exactly one page today (money has five), so the bare-link
// branch that used to render that case was deleted as unreachable rather
// than kept waiting for a space that would use it. Marriage and Family were
// the last spaces in that state; they lost their entries in task 2.
//
// A *builtin* space missing from this map has no built pages at all and
// renders nothing -- the same rule this map already applies one level down
// ("a page joins it once its route exists, not before, because a permanent
// grey 'soon' row reads as broken"). Marriage and Family were both
// rows whose only content was the sentence "Arriving in slice N"; they come
// back here when their pages do. A *custom* space -- one a household made
// with "+ New space" -- is missing from this map permanently and by design,
// so it still renders, as plain text.
const SPACE_PAGES: Record<string, { label: string; to: string }[]> = {
  money: [
    { label: "Finances", to: "/money" },
    { label: "Transactions", to: "/money/transactions" },
    { label: "Budget", to: "/money/budget" },
    { label: "Goals", to: "/money/goals" },
    { label: "Bills", to: "/money/bills" },
  ],
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
// inline-flex items-center: a react-router Link renders an <a>, which is
// inline by default and (unlike <button>/<select>/<input>) never centers its
// own content -- min-h-11 alone would grow the box but leave the text pinned
// to the top. min-h-[auto] (not min-h-0) restores at `lg`, the same
// drawer-tied breakpoint the sign-out button below already uses this
// reasoning for: these links are flex-column children of <nav>'s own
// overflow-y-auto column, where a flex item's *initial* min-height is auto,
// not 0 -- min-h-0 would let a link compress below its content once the
// column itself runs short of room at `lg`, which min-h-[auto] cannot do.
const NAV_ITEM_CLASS =
  "inline-flex min-h-11 items-center rounded-lg px-2.5 py-2 text-[13.5px] font-semibold lg:min-h-[auto]";

function SpaceLink({ space }: { space: Space }) {
  const matchRoute = useMatchRoute();
  const pages = SPACE_PAGES[space.key];
  if (!pages) {
    // Builtin and unbuilt: no row. Custom: a row the household named, with
    // no route yet -- see SPACE_PAGES's comment for why the two differ.
    if (space.isBuiltin) return null;
    return (
      <span data-testid="sidebar-space" className={`${NAV_ITEM_CLASS} text-muted`}>
        {space.name}
      </span>
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
          // 44px floor on phones; restores at `lg`, not `sm` -- this button
          // lives inside the drawer, which switches at `lg`, so a 768px
          // tablet still driving the touch drawer must not fall back to a
          // 24px target.
          className="grid h-11 w-11 flex-none place-items-center rounded-md text-[13px] text-label disabled:opacity-60 lg:h-6 lg:w-6"
        >
          ⏻
        </button>
      </div>
    </nav>
  );
}
