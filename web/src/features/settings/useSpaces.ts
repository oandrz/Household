// Fetch orchestration for SpacesPanel and NewSpaceModal: GET and POST /spaces.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { fetchAndParse } from "../../api/client";
import { spaceSchema } from "../auth/schemas";
import { meQueryKey } from "../auth/useAuth";

export const spacesQueryKey = ["spaces"] as const;

export type VisibilityOption = "everyone" | "parents_only";

const spacesListSchema = z.array(spaceSchema);
type Space = z.infer<typeof spaceSchema>;

async function fetchSpaces(): Promise<Space[]> {
  return fetchAndParse(spacesListSchema, "/api/v1/spaces");
}

export function useSpaces() {
  return useQuery({ queryKey: spacesQueryKey, queryFn: fetchSpaces });
}

async function createSpace(vars: {
  name: string;
  visibility: VisibilityOption;
  template: string;
}) {
  return fetchAndParse(spaceSchema, "/api/v1/spaces", {
    method: "POST",
    body: JSON.stringify(vars),
  });
}

export function useCreateSpace() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: createSpace,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: spacesQueryKey });
      queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}
