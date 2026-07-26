// A direct, deterministic demonstration of the library asymmetry behind a
// real bug found in MagicLinkConsumeScreen during the identity slice's
// definition-of-done walkthrough: the consume request succeeded and the
// session became valid, but the screen never left "Signing you in…", and
// never re-rendered at all after the request settled -- reproduced twice,
// independently, in real Chromium.
//
// Confirmed in the library source: `MutationObserver#notify()`
// (@tanstack/query-core) only invokes the callbacks passed as the second
// argument to `.mutate(variables, options)` while `this.hasListeners()` is
// true -- i.e. only while some component is still actively subscribed to
// that specific observer instance. `Mutation#execute()`, by contrast,
// awaits the mutation's *own* onSuccess/onError (the ones passed to
// `useMutation({...})` itself, not to `.mutate()`) unconditionally, with no
// such gate. MagicLinkConsumeScreen fired its mutate() call from its own
// mount effect; the working comparison case, AcceptInviteForm
// (InviteScreen.tsx), fires the identical per-call pattern from a form
// submit handler instead and works fine.
//
// Not established: exactly why `hasListeners()` was false at the moment
// this particular mutation settled. Dev-mode StrictMode's synchronous
// double-invoke of mount effects is the leading candidate -- it does tear
// down and rebuild every subscription in the tree once, immediately after
// mount -- but that teardown/rebuild is synchronous and should complete
// before an awaited fetch resolves, so it doesn't cleanly account for the
// subscription still being absent later. This test does not depend on
// resolving that; it only pins the asymmetry above.
//
// This exact race did not reproduce under jsdom: a component test wrapped
// in <StrictMode> passed identically whether the bug was present or not
// (confirmed empirically by reverting the fix and re-running it) -- the
// same class of gap the plan's history records for Task 19's Modal
// (jsdom's HTMLDialogElement has no showModal at all, so a real-browser-only
// crash passed every test). So rather than lean on React/jsdom timing that
// has already been shown not to reproduce this, this test exercises the
// underlying library mechanism directly instead. It is NOT a regression
// guard for this specific app-level bug -- someone who reintroduces
// `mutate(vars, { onSuccess })` from a mount effect elsewhere in this
// codebase would leave this test green, because it tests query-core's
// behaviour, not the calling code. What it does guard is the reasoning the
// fix rests on: it pins the library invariant (per-call callbacks are
// listener-gated, hook-level ones aren't) so that reasoning stays correct
// if a future TanStack Query upgrade changes it. The actual defence against
// reintroducing the anti-pattern is the real-browser walkthrough, not this
// file.
import { MutationObserver, QueryClient } from "@tanstack/query-core";
import { describe, expect, it } from "vitest";

describe("MutationObserver per-call callback reliability", () => {
  it("drops the per-call onSuccess once the observer has no listeners, but still runs the mutation's own onSuccess", async () => {
    const client = new QueryClient();
    const hookLevelCalls: unknown[] = [];
    const perCallCalls: unknown[] = [];

    const observer = new MutationObserver(client, {
      mutationFn: async (vars: { value: string }) => {
        // A real turn of the microtask queue between mutate() being called
        // and the mutation settling -- standing in for the network
        // round-trip a real mutationFn awaits.
        await Promise.resolve();
        return vars.value.toUpperCase();
      },
      onSuccess: (data) => hookLevelCalls.push(data),
    });

    const unsubscribe = observer.subscribe(() => {});

    const promise = observer.mutate(
      { value: "ok" },
      { onSuccess: (data) => perCallCalls.push(data) },
    );

    // Unsubscribing here, before the mutationFn's promise resolves, is what
    // StrictMode's synchronous mount-effect teardown does to a component
    // that fires mutate() from its own mount effect: this is the moment its
    // subscription briefly drops before (in production, reliably) coming
    // back.
    unsubscribe();

    await promise;

    expect(hookLevelCalls).toEqual(["OK"]);
    expect(perCallCalls).toEqual([]);
  });

  it("still runs the per-call onSuccess when the observer stays subscribed throughout", async () => {
    const client = new QueryClient();
    const perCallCalls: unknown[] = [];

    const observer = new MutationObserver(client, {
      mutationFn: async (vars: { value: string }) => {
        await Promise.resolve();
        return vars.value.toUpperCase();
      },
    });

    observer.subscribe(() => {});

    await observer.mutate(
      { value: "ok" },
      { onSuccess: (data) => perCallCalls.push(data) },
    );

    expect(perCallCalls).toEqual(["OK"]);
  });
});
