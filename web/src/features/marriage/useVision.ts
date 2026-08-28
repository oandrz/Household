// Fetch orchestration for one household-year's Vision screen -- year-keyed
// the way useRetro.ts keys its own single-month query: a vision is addressed
// by the year it belongs to. Returned field names (`data`, `loading`,
// `error`, `reload`) match useRetro.ts/useRetros.ts rather than an earlier
// draft's own `vision`/`isLoading` -- a reader who already knows one Marriage
// hook should not have to re-learn the next.
//
// Every write here goes through invalidate-and-refetch rather than trusting
// the PUT response directly, the same useRetro.ts/useRetros.ts house
// convention: the GET response is the one place every derived measure figure
// (percent, met, hasFigure) gets computed together, so a screen that read the
// write response directly could disagree with the very next background
// refetch.
//
// saveVision always attaches the version this hook loaded, and the caller
// never supplies one -- a caller that could pass version could pass a stale
// one, and the whole point of the guard (domain.ErrVisionChanged on the
// backend, surfaced here as the VISION_CHANGED conflict below) is that a
// stale save is refused rather than silently overwriting a partner's edits.
//
// `conflict`/`reload` mirror useRetro.ts's own pair: Vision has the identical
// problem retros do. PUT /marriage/vision/{year} answers 409 VISION_CHANGED
// when the version this hook loaded is stale -- either the other partner
// saved first, or two people both opened a never-set year and both hold
// version 0. `conflict` clears on ANY later successful fetch of this year,
// not just an explicit reload() -- an unrelated remount, a refetch-on-focus,
// or reload() itself. Two mechanisms make that true and stay deterministic
// under test, the same pair useRetro.ts uses and explains in full:
//   1. `conflictAt` records query.dataUpdatedAt the moment a save fails with
//      VISION_CHANGED; `conflict` is true while the query's own
//      dataUpdatedAt has not yet moved past that mark. This alone catches a
//      refetch this hook never explicitly drove (window focus, another
//      mount of the same query key).
//   2. Every known success point (saveVision's own onSuccess, and reload())
//      ALSO sets conflictAt to null directly, rather than relying on (1) to
//      notice -- a mocked or simply very fast network can resolve two
//      fetches inside the same tick, in which case dataUpdatedAt never
//      advances between the failed save and the next successful fetch, and
//      (1) alone would leave `conflict` stuck true.
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, apiFetch } from "../../api/client";
import { visionQueryKey } from "./visionQueryKeys";
import { visionResponseSchema, type Vision } from "./visionSchemas";

// SaveVisionBody mirrors saveVisionRequest (vision_handlers.go) minus two
// things: `version`, which this hook attaches itself (see the file header),
// and every server-computed measure field (hasFigure, percent, met,
// goalName) -- a save only ever supplies the inputs those are derived from.
// kind is narrower here than VisionMeasure's own "typed" | "linked" |
// "broken": a household can author a typed or linked measure, but never a
// broken one -- toDomainVision's own comment says that state only ever
// arrives FROM the server, when a linked goal is deleted out from under a
// measure that used to resolve. This is the one place the send shape
// deliberately diverges from the read shape.
export type SaveVisionBody = {
  theme: string;
  description: string;
  pillars: {
    name: string;
    description: string;
    measures: {
      label: string;
      kind: "typed" | "linked";
      current: number;
      target: number;
      goalId: string;
    }[];
  }[];
  milestones: { year: number; title: string; note: string }[];
};

async function fetchVision(year: number): Promise<Vision> {
  const body = await apiFetch<unknown>(`/api/v1/marriage/vision?year=${year}`);
  return visionResponseSchema.parse(body).vision;
}

export function useVision(year: number) {
  const queryClient = useQueryClient();
  const [conflictAt, setConflictAt] = useState<number | null>(null);

  const query = useQuery({
    queryKey: visionQueryKey(year),
    queryFn: () => fetchVision(year),
  });

  const conflict = conflictAt !== null && query.dataUpdatedAt <= conflictAt;

  const saveMutation = useMutation({
    mutationFn: async (body: SaveVisionBody): Promise<Vision> => {
      const current = query.data;
      if (!current) {
        // Fail closed rather than guess a version: nothing loaded yet means
        // there is no real version to attach, and defaulting to 0 would
        // collide with the legitimate "no vision yet" value instead of
        // genuinely meaning it.
        throw new Error("saveVision called before the vision finished loading");
      }
      const raw = await apiFetch<unknown>(`/api/v1/marriage/vision/${year}`, {
        method: "PUT",
        body: JSON.stringify({ ...body, version: current.version }),
      });
      return visionResponseSchema.parse(raw).vision;
    },
    onSuccess: () => {
      setConflictAt(null);
      return queryClient.invalidateQueries({ queryKey: visionQueryKey(year) });
    },
    // VISION_CHANGED means another tab (or the other partner) saved this
    // same year first -- the version this tab loaded is stale.
    // query.dataUpdatedAt here is the timestamp of the load this failed save
    // was based on -- capturing it, not just a bare `true`, is what lets
    // `conflict` clear itself the moment any later fetch resolves, exactly
    // as useRetro.ts's own onError does for RETRO_CHANGED.
    onError: (err) => {
      if (err instanceof ApiError && err.code === "VISION_CHANGED") {
        setConflictAt(query.dataUpdatedAt);
      }
    },
  });

  return {
    data: query.data,
    // v5's `isLoading` is `isPending && isFetching` -- true only while the
    // first fetch for this queryKey is in flight and no cached value exists
    // yet. A background refetch after a save does not flip this back to
    // true, so the screen doesn't blank out on every save.
    loading: query.isLoading,
    error: query.error,
    conflict,
    // Clears conflictAt only once the refetch this triggers actually
    // succeeds -- a reload() that itself fails (offline, a 500) has not
    // shown the caller anything current, so there is nothing yet to call
    // resolved. Exact useRetro.ts precedent.
    reload: async () => {
      const outcome = await query.refetch();
      if (outcome.isSuccess) {
        setConflictAt(null);
      }
    },
    saveVision: (body: SaveVisionBody): Promise<Vision> => saveMutation.mutateAsync(body),
    isSaving: saveMutation.isPending,
    saveError: saveMutation.error,
  };
}
