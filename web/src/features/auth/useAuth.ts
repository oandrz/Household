// TanStack Query hooks over the identity endpoints.
//
// The rule every mutation here follows: when the person signed in on this
// tab changes, nothing the previous person loaded stays in the query cache.
// Query keys are not per-user (["accounts"], ["transactions"]) and a cached
// answer under 30 seconds old is shown without asking the server again, so a
// cache that outlived a change of person would show an owner's balances to
// the limited member who signs in next on a shared tablet, whatever the
// server would have sent them. A new mutation that signs someone in or out
// must end in signedInAs or signedOut below; identityChange.test.tsx fails
// for one that only refreshes ['me'].
import { useState } from "react";
import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { ApiError, apiFetch, fetchAndParse } from "../../api/client";
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
  return fetchAndParse(meQuerySchema, "/api/v1/auth/me");
}

export function useMe() {
  return useQuery({ queryKey: meQueryKey, queryFn: fetchMe });
}

// signedInAs is the one way a mutation records that `me` is now the person
// on this tab.
//
// It removes every inactive query: one that no mounted screen is reading.
// On the screens that sign someone in, the only mounted queries are the
// screen's own (an invite or sign-up preview, the currency list) and ['me'],
// so "inactive" is exactly "left behind by whoever was here before". Don't
// widen this to queryClient.clear(): that also removes the ['me'] query the
// sign-in route is subscribed to, and a subscriber is not told its query
// was replaced, so the route would never see the new person and never leave
// the sign-in screen.
//
// It then stores `me` directly instead of waiting for a refetch: the route
// the caller lands on must not render the previous person's name while one
// is in flight. The invalidate that follows asks the server once more, which
// is what notices a session cookie the browser did not keep.
function signedInAs(queryClient: QueryClient, me: Me) {
  queryClient.removeQueries({ type: "inactive" });
  queryClient.setQueryData(meQueryKey, me);
  queryClient.invalidateQueries({ queryKey: meQueryKey });
}

// signedOut drops every query, ['me'] included. Sign-out happens on a
// signed-in screen whose own queries are still mounted, so "inactive" would
// keep exactly the data that has to go. Leaving ['me'] in place would also
// make /sign-in believe someone is still signed in and send the tab back to
// the home page until the server said otherwise.
function signedOut(queryClient: QueryClient) {
  queryClient.removeQueries();
}

export function useSignIn() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: SignInRequest): Promise<Me> => {
      return fetchAndParse(meQuerySchema, "/api/v1/auth/sign-in", {
        method: "POST",
        body: JSON.stringify(vars),
      });
    },
    onSuccess: (me) => signedInAs(queryClient, me),
  });
}

// The navigation to /sign-in lives here, on the mutation's own onSuccess,
// not in a per-call callback at the button: a per-call callback only runs
// while its component is still subscribed (see useConsumeMagicLink below),
// and a sign-out that emptied the cache but left the tab on a signed-in
// screen would show that screen with nothing behind it.
export function useSignOut() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  return useMutation({
    mutationFn: () => apiFetch<void>("/api/v1/auth/sign-out", { method: "POST" }),
    onSuccess: () => {
      signedOut(queryClient);
      navigate({ to: "/sign-in" });
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

// StartTelegramSignIn mints a deep link into the bot (see
// handleTelegramStart in telegram_handlers.go). Telegram sign-in is
// optional: an install with no bot configured answers 404, and
// SignInScreen hides the control on that response rather than offering a
// button that always fails.
export function useStartTelegramSignIn() {
  return useMutation({
    mutationFn: () =>
      apiFetch<{ url: string; expiresAt: string }>("/api/v1/auth/telegram/start", {
        method: "POST",
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
      return fetchAndParse(
        meQuerySchema,
        "/api/v1/auth/magic-link/consume",
        { method: "POST", body: JSON.stringify(vars) },
      );
    },
    onSuccess: (me) => {
      signedInAs(queryClient, me);
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

  return { mutate: mutation.mutate, isPending: mutation.isPending, errorMessage };
}

export function useAcceptInvite() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: AcceptInviteRequest & { token: string }): Promise<Me> => {
      const { token, ...request } = vars;
      return fetchAndParse(
        meQuerySchema,
        `/api/v1/invites/${encodeURIComponent(token)}/accept`,
        { method: "POST", body: JSON.stringify(request) },
      );
    },
    onSuccess: (me) => signedInAs(queryClient, me),
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
  return fetchAndParse(
    signUpPreviewSchema,
    `/api/v1/auth/sign-up/${encodeURIComponent(token)}`,
  );
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
  return fetchAndParse(currencyListSchema, "/api/v1/currencies");
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
// This route can be reached with a previous session's data still in the
// cache: signUpCompleteRoute deliberately does not bounce an already
// signed-in caller away (router.tsx, for the same shared-device reason
// inviteRoute doesn't). signedInAs is what keeps that data from being shown
// to the household that was just created.
export function useCompleteSignUp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: {
      token: string;
      householdName: string;
      displayName: string;
      primaryCurrency: string;
      timezone: string;
      password: string;
    }): Promise<Me> => {
      const { token, ...request } = vars;
      return fetchAndParse(
        meQuerySchema,
        `/api/v1/auth/sign-up/${encodeURIComponent(token)}/complete`,
        { method: "POST", body: JSON.stringify(request) },
      );
    },
    onSuccess: (me) => signedInAs(queryClient, me),
  });
}
