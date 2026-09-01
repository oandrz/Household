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
import { PowerIcon } from "../../components/icons";

// One entry per built page of each space, in the design's order. A space
// present here renders as the design's uppercase group label plus a link
// per page (the 5a sidebar) -- including a space with only one page (Task 10
// gave Marriage its own single-page turn: money had five pages by the time
// the bare-link branch that used to render a one-page space specially was
// deleted as unreachable, but that deletion did not remove the *ability* to
// render one page this way -- the group-label-plus-links branch below
// already handles `pages.length === 1` identically to `length > 1`, so
// Marriage needed no code change here, only this entry).
//
// A *builtin* space missing from this map has no built pages at all and
// renders nothing -- the same rule this map already applies one level down
// ("a page joins it once its route exists, not before, because a permanent
// grey 'soon' row reads as broken"). Family is still in that state: its own
// "Arriving in slice N" row was deleted in `110ab0a` and nothing has
// rebuilt it yet. A *custom* space -- one a household made with "+ New
// space" -- is missing from this map permanently and by design, so it still
// renders, as plain text.
const SPACE_PAGES: Record<string, { label: string; to: string }[]> = {
  money: [
    { label: "Finances", to: "/money" },
    { label: "Transactions", to: "/money/transactions" },
    { label: "Budget", to: "/money/budget" },
    { label: "Goals", to: "/money/goals" },
    { label: "Bills", to: "/money/bills" },
  ],
  // Task 10 -- Retros is the first of Marriage's three pages
  // (docs/FEATURE_TRACKER.md section 6). This is the entry `110ab0a` deleted
  // alongside `/marriage`'s own route and guard; all three came back
  // together (see router.tsx's own header comment on why splitting them
  // across tasks isn't safe). Task 11 added Vision & goals as the second --
  // Agreements is still ⬜.
  marriage: [
    { label: "Retros", to: "/marriage/retros" },
    { label: "Vision & goals", to: "/marriage/vision" },
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

// Background, never a hover:text-* -- the comment above explains why a second
// colour class on these links cannot be relied on to win the cascade. A
// background is a different CSS property, so it never enters that fight.
// Kept out of NAV_ITEM_CLASS itself (that constant's own comment: "Layout
// only -- no color here," and a background is a colour) and applied
// unconditionally, not gated on `isActive`, so it can never become a second
// conditional class fighting the active colour the way text-accent/text-ink
// already do.
const NAV_ITEM_STATE_CLASS =
  "transition-colors duration-[var(--transition-state)] hover:bg-canvas active:bg-toggle-off";

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
            className={`${NAV_ITEM_CLASS} ${NAV_ITEM_STATE_CLASS} ${isActive ? "text-accent" : "text-ink"}`}
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
    // flex-1: NavDrawer's panel is h-dvh and flex-col below `lg`, and this
    // <nav> is its only child -- without flex-1 the panel's box is full
    // height but the nav inside it is only content-height, leaving a gap at
    // the bottom. Neither the panel nor that gap paints a background, so the
    // drawer's backdrop shows through it visually, but the panel's own box
    // still covers the point -- a tap there hits the panel, not the
    // backdrop, so it looks dismissable and isn't. At `lg` the panel is
    // `display: contents` (see NavDrawer), so this <nav> is a direct grid
    // child there instead and stretches to the row's height regardless --
    // flex-1 has no effect on a grid item and does not change the desktop
    // column.
    <nav className="flex flex-1 flex-col gap-0.5 overflow-y-auto border-r border-hairline bg-card px-4 py-[22px]">
      {/* The design's sidebar has no separate top bar; this brand row (logo
          square, "Hearth") is the only header it draws, so AppShell's
          "header" is this.

          The design also draws a ⌘K chip here. It is deliberately not
          rendered: no command palette exists to open, and the chip appeared
          in the mobile drawer too, on devices with no ⌘ key. It comes back
          the day a palette does -- so do not re-add it from the design
          file. */}
      <div className="flex items-center gap-2.5 px-2.5 pb-[18px]">
        <div className="h-7 w-7 rounded-lg bg-accent" />
        <div className="text-[15px] font-semibold tracking-[-0.01em]">
          Hearth
        </div>
      </div>

      <Link
        to="/"
        className={`${NAV_ITEM_CLASS} ${NAV_ITEM_STATE_CLASS} ${overviewActive ? "text-accent" : "text-ink"}`}
      >
        Overview
      </Link>

      {me.spaces.map((space) => (
        <SpaceLink key={space.id} space={space} />
      ))}

      <div className="flex-1" />

      <Link
        to="/settings"
        className={`${NAV_ITEM_CLASS} ${NAV_ITEM_STATE_CLASS} ${settingsActive ? "text-accent" : "text-ink"}`}
      >
        Settings
      </Link>

      {/* Not a space, so it does not go through SPACE_PAGES or me.spaces --
          this is the one person running the platform, not a household
          feature. Rendered only for that flag; the server's own
          requirePlatformAdmin is what actually refuses a non-admin (with a
          404, not a 403 -- see middleware_admin.go), this is only the
          courtesy of not showing a door that opens onto one.
          NAV_ITEM_CLASS/NAV_ITEM_STATE_CLASS, the same pair every other nav
          link carries -- this link was shipped with only a hand-picked
          subset (no padding, no rounded hit-target, no hover/active state,
          missing the lg:min-h-[auto] restore) and read as visibly
          inconsistent beside them. `mt-6` stays, on top of those two, as
          the one thing that *should* set this link apart: it is not a
          space, so it does not belong in the run of space links above it.
          Still exactly one colour class -- text-muted -- per the comment on
          NAV_ITEM_CLASS: layout and state classes carry no colour of their
          own, so there is nothing here for the cascade to arbitrate. */}
      {me.isPlatformAdmin && (
        <Link to="/admin" className={`${NAV_ITEM_CLASS} ${NAV_ITEM_STATE_CLASS} mt-6 text-muted`}>
          Admin
        </Link>
      )}

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
          className="grid h-11 w-11 flex-none place-items-center rounded-md text-[13px] text-label transition-colors duration-[var(--transition-state)] enabled:hover:bg-canvas enabled:active:bg-toggle-off disabled:opacity-60 lg:h-6 lg:w-6"
        >
          <PowerIcon />
        </button>
      </div>
    </nav>
  );
}
