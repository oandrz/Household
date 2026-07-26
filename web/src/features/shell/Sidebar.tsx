// Renders from `me.spaces`, never a hard-coded list -- that is the property
// that lets "+ New space" (Task 20) extend the navigation without a change
// here. The server has already filtered this array by the caller's
// capabilities and ordered it by position (domain.VisibleSpaces); this
// component renders it exactly as given, in that order, and does not
// re-sort or re-filter it.
//
// The design's 5a sidebar groups each space into an uppercase label plus
// several sub-page links (Finances/Transactions/Budget/... under Money, and
// so on) -- those sub-pages belong to slices 2-4, which haven't been built
// yet. Until they exist, one space has exactly one destination, so each
// space renders as a single clickable nav item in the same style as
// Overview/Settings, not as an inert group label.
import { Link, useNavigate } from "@tanstack/react-router";
import type { Me, Space } from "../auth/schemas";
import { useSignOut } from "../auth/useAuth";

// The only spaces with a route built this task (docs/superpowers/specs/
// 2026-07-26-hearth-foundation-design.md puts custom space pages out of
// scope). A space key this map doesn't recognise -- reachable once "+ New
// space" lets a household create one -- renders as plain text rather than a
// Link to a route that doesn't exist.
const SPACE_PATHS: Record<
  string,
  "/money" | "/marriage" | "/family/calendar"
> = {
  money: "/money",
  marriage: "/marriage",
  family: "/family/calendar",
};

const NAV_ITEM_CLASS =
  "rounded-lg px-2.5 py-2 text-[13.5px] font-semibold text-ink";

function SpaceLink({ space }: { space: Space }) {
  const to = SPACE_PATHS[space.key];
  if (!to) {
    return (
      <span data-testid="sidebar-space" className={`${NAV_ITEM_CLASS} text-muted`}>
        {space.name}
      </span>
    );
  }
  return (
    <Link data-testid="sidebar-space" to={to} className={NAV_ITEM_CLASS}>
      {space.name}
    </Link>
  );
}

export function Sidebar({ me }: { me: Me }) {
  const signOut = useSignOut();
  const navigate = useNavigate();

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

      <Link to="/" className={`${NAV_ITEM_CLASS} text-accent`}>
        Overview
      </Link>

      {me.spaces.map((space) => (
        <SpaceLink key={space.id} space={space} />
      ))}

      <div className="flex-1" />

      <Link to="/settings" className={NAV_ITEM_CLASS}>
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
