// Positions the sidebar: off-canvas below `lg`, and out of the way entirely
// at `lg` and above. It holds no navigation content -- Sidebar.tsx renders
// that, exactly once, and this wraps that single instance rather than a
// second copy (two copies would put two data-testid="sidebar-space" elements
// in the DOM and make every query in Sidebar.test.tsx ambiguous).
//
// Why not a native <dialog>, when components/Modal makes such a point of
// using one for free focus trapping and Escape handling: the same element
// here must also be the *static desktop column*, and a <dialog> cannot be
// one. So the three things <dialog> would have given are written by hand
// below -- Escape, focus in, focus back -- and NavDrawer.test.tsx covers all
// three because hand-written versions are exactly what regresses silently.
//
// `lg:contents` is what guarantees the desktop layout is untouched: at `lg`
// this wrapper stops generating a box at all, so Sidebar becomes the grid
// child of AppShell's two-column grid again, precisely as it was before this
// component existed. `position` and `transform` have no effect on an element
// with `display: contents`, so neither needs an `lg:` counterpart.
//
// `visibility` is the exception, and it is the one that bites: it is an
// *inherited* property, and `display: contents` suppresses the box, not the
// inheritance. Without the `lg:visible` below, the desktop sidebar would
// inherit `visibility: hidden` from this element in its normal state --
// closed -- and render as an invisible 236px column. Hence `lg:visible` on
// the closed branch specifically.
//
// Visibility, not aria-hidden, is what hides the closed drawer: `invisible`
// removes it from the tab order and the accessibility tree together, and it
// is a CSS rule, so it responds to the viewport without any JavaScript
// needing to know the width. An aria-hidden attribute would have to be
// driven from JS and would wrongly hide the visible desktop sidebar.
import { type ReactNode, useEffect, useRef } from "react";

export function NavDrawer({
  open,
  onClose,
  children,
}: {
  open: boolean;
  onClose: () => void;
  children: ReactNode;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const previouslyFocusedRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (!open) return;

    // The same save-and-restore components/Modal performs, and for the same
    // reason: a keyboard user who opens the drawer and closes it again must
    // land back on the control they pressed, not at the top of the document.
    previouslyFocusedRef.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    panelRef.current?.focus();

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") onClose();
    }
    document.addEventListener("keydown", handleKeyDown);

    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      const previouslyFocused = previouslyFocusedRef.current;
      if (previouslyFocused && document.contains(previouslyFocused)) {
        previouslyFocused.focus();
      }
    };
  }, [open, onClose]);

  return (
    <>
      {open && (
        <div
          data-testid="nav-drawer-backdrop"
          onClick={onClose}
          className="fixed inset-0 z-30 bg-black/40 lg:hidden"
        />
      )}
      <div
        ref={panelRef}
        tabIndex={-1}
        data-testid="nav-drawer"
        className={`fixed left-0 top-0 z-40 flex h-dvh w-[236px] flex-col overflow-y-auto transition-transform lg:contents ${
          open ? "visible translate-x-0" : "invisible -translate-x-full lg:visible"
        }`}
      >
        {children}
      </div>
    </>
  );
}
