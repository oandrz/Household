// TanStack Query hooks over the identity endpoints. Every mutation that
// changes who the caller is signed in as invalidates ['me'] so the rest of
// the app (the sidebar, once Task 19 builds it) picks up the change.
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ApiError, apiFetch } from "../../api/client";
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
//
// Both onSuccess and onError are wired here, on the mutation's own options,
// rather than passed as the second (per-call) argument to `.mutate()` the
// way MagicLinkConsumeScreen used to. That distinction is load-bearing, not
// stylistic.
//
// What's confirmed, by instrumenting both paths directly in three separate,
// independent real-Chromium reproductions: the mutation's own `onSuccess`
// here always fired; a per-call `mutate(vars, { onSuccess })` attached from
// MagicLinkConsumeScreen's mount effect never did, and the screen never
// re-rendered after the request settled at all.
//
// What's confirmed in the library source: `MutationObserver#mutate(vars,
// options)` (query-core/src/mutationObserver.ts) stores `options` and its
// `#notify()` only invokes it while `this.hasListeners()` is true --i.e.
// only while some component is still subscribed to *this* observer
// instance. `Mutation#execute()` (query-core/src/mutation.ts), by contrast,
// awaits `this.options.onSuccess` / `this.options.onError` -- the ones set
// here -- unconditionally, with no such gate. That asymmetry, not any
// particular theory of why the gate tripped, is why side effects belong
// here rather than on a per-call options object: this path is reliable
// regardless of subscription state at settle time, the other is not.
//
// Not confirmed: the exact reason `hasListeners()` was false at the moment
// this fired from a mount effect. Dev-mode StrictMode's synchronous
// double-invoke of mount effects (tearing down and rebuilding every
// subscription in the tree once, including this hook's own
// `useSyncExternalStore`) is the leading candidate and matches the
// unmount/remount trace observed, but the teardown and rebuild happen
// synchronously back-to-back, before any awaited fetch can settle, so it
// doesn't cleanly explain a subscription still being absent later when the
// mutation resolves. Left open rather than asserted, because the fix here
// doesn't depend on resolving it -- it depends only on the asymmetry above.
// The working comparison case is AcceptInviteForm (InviteScreen.tsx), which
// uses the same per-call pattern successfully because it fires from a form
// submit handler, not a mount effect.
//
// The same gate that swallows a per-call onSuccess would equally swallow a
// per-call onError, so the error path is handled the same way: `errorMessage`
// is plain React state, updated from this reliable onError, rather than read
// reactively off the mutation's own listener-gated result (`consume.isError`
// / `consume.error`), which is not guaranteed to prompt a re-render for the
// same reason.
export function useConsumeMagicLink() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: async (vars: { token: string }): Promise<Me> => {
      const body = await apiFetch<unknown>(
        "/api/v1/auth/magic-link/consume",
        { method: "POST", body: JSON.stringify(vars) },
      );
      return meQuerySchema.parse(body);
    },
    onSuccess: (data) => {
      // setQueryData primes the cache directly and synchronously from this
      // mutation's own response -- invalidateQueries alone isn't enough
      // here: it only forces a refetch of *active* observers, and nothing
      // on the /sign-in/magic route mounts a useMe() of its own. Kept
      // alongside setQueryData anyway, for any other tab/observer sharing
      // this queryClient that might already be watching ['me'].
      queryClient.setQueryData(meQueryKey, data);
      queryClient.invalidateQueries({ queryKey: meQueryKey });
      navigate({ to: "/", replace: true });
    },
    onError: (err) => {
      setErrorMessage(
        err instanceof ApiError
          ? err.message
          : "That link didn't work. Please try again.",
      );
    },
  });

  return { mutate: mutation.mutate, errorMessage };
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
