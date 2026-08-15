// The Spaces panel (design/Household Dashboard.dc.html's Settings screen,
// the second card). Reads GET /spaces -- the same domain.VisibleSpaces
// result Sidebar.tsx renders from `me.spaces` -- through its own query
// instead, so this panel's own mutation (space creation) has its own cache
// entry to invalidate, per the task's "each panel owns its own mutation and
// query" instruction.
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { z } from "zod";
import { apiFetch } from "../../api/client";
import { spaceSchema } from "../auth/schemas";
import { useMe } from "../auth/useAuth";
import { spaceAudienceLabel } from "./copy";
import { NewSpaceModal } from "./NewSpaceModal";

const spacesListSchema = z.array(spaceSchema);
type Space = z.infer<typeof spaceSchema>;

async function fetchSpaces(): Promise<Space[]> {
  const body = await apiFetch<unknown>("/api/v1/spaces");
  return spacesListSchema.parse(body);
}

function useSpaces() {
  return useQuery({ queryKey: ["spaces"], queryFn: fetchSpaces });
}

export function SpacesPanel() {
  const me = useMe();
  const spaces = useSpaces();
  const [newSpaceOpen, setNewSpaceOpen] = useState(false);
  const isOwner = me.data?.membership.role === "owner";

  return (
    <section className="rounded-xl border border-hairline bg-card p-[22px]">
      <h2 className="mb-4 text-sm font-semibold text-ink">Spaces</h2>

      {spaces.isPending && <p className="text-xs text-muted">Loading…</p>}
      {spaces.isError && (
        <p role="alert" className="text-xs text-danger">
          Couldn't load the spaces list.
        </p>
      )}

      {spaces.isSuccess && (
        <div className="flex flex-col gap-2.5 text-[13px]">
          {spaces.data.map((space) => (
            <div
              key={space.id}
              className="flex items-center justify-between rounded-lg bg-surface px-3 py-2.5"
            >
              {/* The design's rows also carry a "· N pages" count -- there
                  is no backend field for how many pages a space has (this
                  slice hasn't built any of Money/Marriage/Family's
                  sub-pages yet), so, like the members list's omitted age
                  suffix, it is left out rather than invented. */}
              <span className="text-ink">{space.name}</span>
              <span className="text-muted">{spaceAudienceLabel(space)}</span>
            </div>
          ))}

          {isOwner && (
            <button
              type="button"
              onClick={() => setNewSpaceOpen(true)}
              // min-h-11/sm:min-h-0: TransactionFilters.tsx's own
              // SELECT_CLASS comment has the measured reason py-2.5 alone
              // falls short of the 44px floor on a phone.
              className="flex min-h-11 items-center gap-2 rounded-lg border border-dashed border-hairline px-3 py-2.5 text-left text-muted sm:min-h-0"
            >
              + New space (Kids, Home, Travel…)
            </button>
          )}
        </div>
      )}

      <NewSpaceModal open={newSpaceOpen} onClose={() => setNewSpaceOpen(false)} />
    </section>
  );
}
