// TanStack Query hooks over the identity endpoints. Every mutation that
// changes who the caller is signed in as invalidates ['me'] so the rest of
// the app (the sidebar, once Task 19 builds it) picks up the change.
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import {
  meQuerySchema,
  type AcceptInviteRequest,
  type Me,
  type MagicLinkRequest,
  type SignInRequest,
} from "./schemas";

export const meQueryKey = ["me"] as const;

async function fetchMe(): Promise<Me> {
  const body = await apiFetch<unknown>("/api/v1/auth/me");
  return meQuerySchema.parse(body);
}

export function useMe() {
  return useQuery({ queryKey: meQueryKey, queryFn: fetchMe });
}

export function useSignIn() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: SignInRequest): Promise<Me> => {
      const body = await apiFetch<unknown>("/api/v1/auth/sign-in", {
        method: "POST",
        body: JSON.stringify(vars),
      });
      return meQuerySchema.parse(body);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}

export function useSignOut() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: () => apiFetch<void>("/api/v1/auth/sign-out", { method: "POST" }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}

// RequestMagicLink always answers 202 {"status":"accepted"} whether or not
// the address exists or the rate limit is exhausted, and the send itself is
// fire-and-forget on the backend -- see usecase/auth.go. There is nothing
// here to invalidate: an unconsumed magic link has not changed who the
// caller is.
export function useRequestMagicLink() {
  return useMutation({
    mutationFn: (vars: MagicLinkRequest) =>
      apiFetch<{ status: string }>("/api/v1/auth/magic-link", {
        method: "POST",
        body: JSON.stringify(vars),
      }),
  });
}

// ConsumeMagicLink signs the token's holder in -- see usecase/auth.go. The
// token is single-use, so this must only ever be called once per emailed
// link; MagicLinkConsumeScreen guards against StrictMode's double-invoke of
// effects, not this hook itself.
export function useConsumeMagicLink() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { token: string }): Promise<Me> => {
      const body = await apiFetch<unknown>(
        "/api/v1/auth/magic-link/consume",
        { method: "POST", body: JSON.stringify(vars) },
      );
      return meQuerySchema.parse(body);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}

export function useAcceptInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: AcceptInviteRequest & { token: string }): Promise<Me> => {
      const { token, ...request } = vars;
      const body = await apiFetch<unknown>(
        `/api/v1/invites/${encodeURIComponent(token)}/accept`,
        { method: "POST", body: JSON.stringify(request) },
      );
      return meQuerySchema.parse(body);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}
