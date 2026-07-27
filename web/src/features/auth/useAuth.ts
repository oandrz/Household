// TanStack Query hooks over the identity endpoints. Every mutation that
// changes who the caller is signed in as invalidates ['me'] so the rest of
// the app (the sidebar, once Task 19 builds it) picks up the change.
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ApiError, apiFetch } from "../../api/client";
import {
  currencyListSchema,
  meQuerySchema,
  signUpPreviewSchema,
  type AcceptInviteRequest,
  type Currency,
  type Me,
  type MagicLinkRequest,
  type SignInRequest,
  type SignUpPreview,
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

// POST /auth/sign-up always answers 202 with the same body, whether or not
// the address has an account (see signup_handlers.go's handleSignUp doc
// comment -- byte-identical to handleRequestMagicLink's answer). Nothing
// here can tell the difference, and nothing should try: the screen says
// "check your email" either way.
export function useRequestSignUp() {
  return useMutation({
    mutationFn: (vars: { email: string }) =>
      apiFetch<{ status: string }>("/api/v1/auth/sign-up", {
        method: "POST",
        body: JSON.stringify(vars),
      }),
  });
}

async function fetchSignUpPreview(token: string): Promise<SignUpPreview> {
  const body = await apiFetch<unknown>(
    `/api/v1/auth/sign-up/${encodeURIComponent(token)}`,
  );
  return signUpPreviewSchema.parse(body);
}

export function useSignUpPreview(token: string) {
  return useQuery({
    queryKey: ["sign-up-preview", token] as const,
    queryFn: () => fetchSignUpPreview(token),
    // A spent or expired token will never become valid by retrying, and each
    // retry costs the caller a wait before they see the message telling them
    // what to do instead.
    retry: false,
  });
}

async function fetchCurrencies(): Promise<{ currencies: Currency[] }> {
  const body = await apiFetch<unknown>("/api/v1/currencies");
  return currencyListSchema.parse(body);
}

// Consumed by the sign-up completion screen's currency select (Task 31); the
// endpoint itself is public (see currency_handlers.go), so this can be
// called before any session exists.
export function useCurrencies() {
  return useQuery({
    queryKey: ["currencies"] as const,
    queryFn: fetchCurrencies,
    // The active ISO 4217 list does not change during a session.
    staleTime: Infinity,
  });
}

// completeSignUp provisions the household and signs the new owner in through
// the same completeSignIn tail sign-in, magic-link consumption and invite
// acceptance use (see signup_handlers.go's handleCompleteSignUp), so all four
// answer with the identical me bundle and the identical pair of cookies.
//
// Seeds ['me'] with setQueryData rather than only invalidating -- matching
// useConsumeMagicLink above, not useSignIn/useAcceptInvite's invalidate-only,
// because this route can be reached with a *stale* ['me'] already sitting in
// the cache. signUpCompleteRoute deliberately does not bounce an
// already-signed-in caller away (router.tsx, for the same shared-device
// reason inviteRoute doesn't), so this screen is reachable via same-tab
// navigation from an authenticated screen, within the default 5-minute
// gcTime of that previous session's ['me'] query. invalidateQueries alone
// only marks an *inactive* query stale -- it does not refetch it -- so a
// fresh useMe observer that mounts right after (e.g. AppShell, once this
// screen navigates to "/") would synchronously render that stale,
// previous-owner data (isPending: false, wrong household) before the
// background refetch its own staleness triggers has resolved. setQueryData
// replaces the cache with this response's own bundle up front, so there is
// nothing stale left to flash. invalidateQueries is kept alongside it
// anyway, for any other tab/observer sharing this queryClient that might
// already be watching ['me'].
export function useCompleteSignUp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: {
      token: string;
      householdName: string;
      displayName: string;
      primaryCurrency: string;
      password: string;
    }): Promise<Me> => {
      const { token, ...request } = vars;
      const body = await apiFetch<unknown>(
        `/api/v1/auth/sign-up/${encodeURIComponent(token)}/complete`,
        { method: "POST", body: JSON.stringify(request) },
      );
      return meQuerySchema.parse(body);
    },
    onSuccess: (data) => {
      queryClient.setQueryData(meQueryKey, data);
      queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}
