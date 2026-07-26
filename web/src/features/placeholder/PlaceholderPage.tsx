// Every destination this slice doesn't build yet routes here instead of
// crashing or rendering blank -- an unfinished area should say so honestly.
// `slice` names the plan's own numbering (see
// docs/superpowers/specs/2026-07-26-hearth-foundation-design.md's slices
// table): 2 Money, 3 Marriage, 4 Family, 5 Overview. Settings (slice 1, this
// same slice) also renders here until Task 20 replaces this route's
// component with the real Settings screen.
export function PlaceholderPage({
  page,
  slice,
}: {
  page: string;
  slice: number;
}) {
  return (
    <div className="flex flex-col gap-2 p-10">
      <h1 className="font-serif text-2xl">{page}</h1>
      <p className="text-sm text-muted">Arriving in slice {slice}.</p>
    </div>
  );
}
