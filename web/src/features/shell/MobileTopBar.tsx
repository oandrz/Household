// The mobile counterpart to Sidebar's brand row. Below `lg` the sidebar sits
// off-canvas (see NavDrawer), so this row holds the only control that can
// bring it back -- which is why it is sticky rather than static: a ledger
// scrolled two screens down would otherwise leave a phone with no way to
// navigate at all.
//
// The design has no mobile artwork to follow (design/Household Dashboard.dc.html
// is a 1440px canvas with zero media queries), so this row is invention,
// authorised by the product owner on 2026-08-15 under "same UI, layout and
// structure -- invent where a phone forces a choice". It repeats the
// sidebar's own brand mark rather than introducing a second one.
//
// The sidebar's ⌘K chip is deliberately absent: it opens nothing (no command
// palette exists), and on 375px it would cost width that a product name and a
// 44px touch target both need.
export function MobileTopBar({ onOpenNav }: { onOpenNav: () => void }) {
  return (
    <header className="sticky top-0 z-30 flex items-center gap-2.5 border-b border-hairline bg-card px-4 py-2.5 lg:hidden">
      <div className="h-7 w-7 rounded-lg bg-accent" />
      <div className="text-[15px] font-semibold tracking-[-0.01em]">Hearth</div>
      <button
        type="button"
        onClick={onOpenNav}
        aria-label="Open navigation"
        className="ml-auto grid h-11 w-11 place-items-center rounded-lg border border-hairline text-[15px] text-ink"
      >
        ☰
      </button>
    </header>
  );
}
