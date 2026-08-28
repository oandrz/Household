// Fetch orchestration for one household-year's Vision screen -- year-keyed
// the way useRetro.ts keys its own single-month query: a vision is addressed
// by the year it belongs to. Every write here goes through invalidate-and-
// refetch rather than trusting the PUT response directly, the same
// useRetro.ts/useRetros.ts house convention: the GET response is the one
// place every derived measure figure (percent, met, hasFigure) gets
// computed together, so a screen that read the write response directly could
// disagree with the very next background refetch.
//
// saveVision always attaches the version this hook loaded, and the caller
// never supplies one -- a caller that could pass version could pass a stale
// one, and the whole point of the guard (domain.ErrVisionChanged on the
// backend) is that a stale save is refused rather than silently overwriting
// a partner's edits.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
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

  const query = useQuery({
    queryKey: visionQueryKey(year),
    queryFn: () => fetchVision(year),
  });

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
    onSuccess: () => queryClient.invalidateQueries({ queryKey: visionQueryKey(year) }),
  });

  return {
    vision: query.data,
    // v5's `isLoading` is `isPending && isFetching` -- true only while the
    // first fetch for this queryKey is in flight and no cached value exists
    // yet. A background refetch after a save does not flip this back to
    // true, so the screen doesn't blank out on every save.
    isLoading: query.isLoading,
    error: query.error,
    saveVision: (body: SaveVisionBody): Promise<Vision> => saveMutation.mutateAsync(body),
    isSaving: saveMutation.isPending,
    saveError: saveMutation.error,
  };
}
