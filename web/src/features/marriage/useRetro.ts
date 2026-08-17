// Fetch orchestration for one month's Retro detail screen -- month-keyed
// the way useBudget.ts keys its own single-month query (`retroQueryKey(month)`),
// not useGoals.ts's include-archived union shape: a retro detail is
// addressed by which month it belongs to, never toggled by a flag. Every
// write below awaits both this hook's own key and the history list's key
// (retroListQueryKey) before resolving -- a save, finish, discard or action
// write moves the list's own mood point, actionCount, quote and finished
// flag too, not just this screen, and CurrencyPanel / NotificationsPanel
// are on the follow-up list for exactly the non-awaited version of that.
// Both query keys live in retroQueryKeys.ts, not here or in useRetros.ts --
// see that file's own header comment for why.
import { useState } from "react";
import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { ApiError, apiFetch } from "../../api/client";
import { retroListQueryKey, retroQueryKey } from "./retroQueryKeys";
import {
  retroActionResponseSchema,
  retroActionTickResponseSchema,
  retroDetailResponseSchema,
  retroWriteResponseSchema,
  type RetroAction,
  type RetroDetailResponse,
} from "./retroSchemas";

async function fetchRetro(month: string): Promise<RetroDetailResponse> {
  const body = await apiFetch<unknown>(`/api/v1/retros/${encodeURIComponent(month)}`);
  return retroDetailResponseSchema.parse(body);
}

// SaveRetroBody is a partial patch of PATCH /retros/{month}'s own fields.
// Unlike updateGoalRequest's pointers (an absent key means "unchanged" on
// the wire), saveRetroRequest is always a full replace -- there is no
// per-field "unchanged" sentinel server-side (saveRetroRequest's own
// comment: "the modal always sends all three text fields plus mood ...
// there is no per-field unchanged sentinel"). saveRetro below fills in
// whatever a caller omits from the retro this hook already has loaded, and
// always attaches the version it loaded itself -- the caller never supplies
// version, which is the point of the mutation check in useRetros.test.ts: a
// hook that let the caller pass version could send a stale one by accident.
//
// The fill-in source is the last-loaded SERVER state (query.data), never
// anything the caller may currently have typed and not passed here -- a
// caller that only sends { mood } will overwrite wentWell/wasHard/notes
// with whatever the server last answered, not with unsaved on-screen text.
// Task 13's modal, which holds all four fields in local state, must pass
// all four on every save for this reason; it cannot rely on this merge to
// preserve an edited textarea it left out.
export type SaveRetroBody = Partial<{
  mood: number | null;
  wentWell: string;
  wasHard: string;
  notes: string;
}>;

// AddRetroActionBody mirrors addRetroActionRequest. assigneeMembershipIds
// and carriedFrom are both optional here (Task 8's own note: "omit or
// []/\"\""), filled in as [] / "" below before the request goes out so the
// wire body always matches the shape retroActionSchema itself requires back.
//
// Whoever builds the carry-over control (Task 13/14): `carriedFrom` here
// MUST only ever be an id taken from this retro's own `carryOver` list
// (RetroDetailResponse's sibling field) -- never a freehand or historical
// action id. RetroDetail's own "Carried from {month}" label (retroCopy.ts's
// `previousMonthName`) infers the source month from this retro's own month
// minus one, because the backend does not return which month a carriedFrom
// id actually belongs to; passing anything but that list's own ids breaks
// the label silently, not loudly. Full reasoning is on `previousMonthName`.
export type AddRetroActionBody = {
  body: string;
  assigneeMembershipIds?: string[];
  carriedFrom?: string;
};

function invalidateAfterRetroWrite(queryClient: QueryClient, month: string) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: retroQueryKey(month) }),
    queryClient.invalidateQueries({ queryKey: retroListQueryKey() }),
  ]);
}

// useRetro(month) loads one month's detail screen (the retro, its own
// actions, and last month's still-open carry-over offer) and exposes every
// month-scoped write. `conflict` clears on ANY later successful resolve of
// this query, not on a fixed list of call sites -- an explicit reload(), a
// different write's own invalidateQueries (finishRetro/discardDraft/
// addAction/setActionDone/removeAction all go through the same afterWrite
// helper below, not just saveRetro), or even a background
// refetch-on-focus. Two mechanisms cooperate to make that true and stay
// deterministic under test:
//
//   1. `conflictAt` records query.dataUpdatedAt at the moment saveRetro
//      last failed with RETRO_CHANGED; `conflict` is derived as true while
//      the query's own dataUpdatedAt has not yet moved past that mark.
//      This alone is what catches a refetch this hook never explicitly
//      drove itself (window focus, another mount of the same query key).
//   2. Every known success point (every mutation's afterWrite, and
//      reload()) ALSO sets conflictAt to null directly, rather than
//      relying on (1) to notice. A mocked -- or simply very fast -- network
//      can resolve two fetches inside the same tick, in which case
//      dataUpdatedAt never advances between the failed save and the next
//      successful fetch, and (1) alone would leave `conflict` stuck true;
//      an explicit null does not depend on a timestamp changing at all.
//
// An effect watching isFetching/isSuccess transitions was tried and
// rejected first: the same same-tick resolution that defeats a bare
// dataUpdatedAt comparison also collapses a fetching->idle transition
// before any effect gets to observe it.
//
// NOTHING IN THE APP READS `conflict` OR CALLS `reload()`. This comment used
// to say the modal reads `conflict` to render its banner; it does not. The
// modal decides a conflict itself (`err.code === "RETRO_CHANGED"`) and drives
// a one-way `hadConflict` latch, because a two-way flag is what made the
// removed Reload control dangerous: clearing it re-enabled Save with stale
// local fields attached to a fresh version, which is last-write-wins — the
// exact loss the version guard exists to prevent, and a browser walk caught it
// (see retroCopy.ts's conflictBanner comment).
//
// So `conflict` and `reload` are kept only because their derivation is tested
// and documents the shape; adding a caller is NOT safe on its own. Any UI that
// clears a conflict must also re-seed the editor from the server's version
// first, or it reintroduces that defect.
export function useRetro(month: string) {
  const queryClient = useQueryClient();
  const [conflictAt, setConflictAt] = useState<number | null>(null);

  const query = useQuery({
    queryKey: retroQueryKey(month),
    queryFn: () => fetchRetro(month),
  });

  const conflict = conflictAt !== null && query.dataUpdatedAt <= conflictAt;

  // Shared onSuccess for every month-scoped write below, saveRetro
  // included (saveRetro only adds its own onError beside this, for
  // RETRO_CHANGED): clear conflictAt outright, then invalidate both this
  // hook's own key and the history list's, awaited so the caller's
  // mutateAsync doesn't resolve until both have settled.
  function afterWrite() {
    setConflictAt(null);
    return invalidateAfterRetroWrite(queryClient, month);
  }

  const saveMutation = useMutation({
    mutationFn: async (body: SaveRetroBody) => {
      const current = query.data;
      if (!current) {
        // Fail closed rather than send a guessed version: a caller with
        // nothing loaded yet has no version to attach, and 0/undefined
        // would never match the server's real one either.
        throw new Error("saveRetro called before the retro finished loading");
      }
      const payload = {
        // `!== undefined`, not `??`: null is a legitimate, explicit "clear
        // the mood" value (saveRetroRequest's own comment), so it must pass
        // through rather than falling back to the currently loaded mood the
        // way `??` would.
        mood: body.mood !== undefined ? body.mood : current.retro.mood,
        wentWell: body.wentWell ?? current.retro.wentWell,
        wasHard: body.wasHard ?? current.retro.wasHard,
        notes: body.notes ?? current.retro.notes,
        version: current.retro.version,
      };
      const raw = await apiFetch<unknown>(`/api/v1/retros/${encodeURIComponent(month)}`, {
        method: "PATCH",
        body: JSON.stringify(payload),
      });
      return retroWriteResponseSchema.parse(raw).retro;
    },
    onSuccess: afterWrite,
    // RETRO_CHANGED means another tab saved this same retro first -- the
    // version this tab loaded is stale. Recorded as `conflictAt` rather
    // than left for the caller's own catch block to interpret: Task 13's
    // modal needs to tell this apart from every other kind of failure.
    // query.dataUpdatedAt here is the timestamp of the load this failed
    // save was based on -- capturing it, not just a bare `true`, is what
    // lets `conflict` clear itself the moment any later fetch resolves.
    onError: (err) => {
      if (err instanceof ApiError && err.code === "RETRO_CHANGED") {
        setConflictAt(query.dataUpdatedAt);
      }
    },
  });

  const finishMutation = useMutation({
    mutationFn: async () => {
      const raw = await apiFetch<unknown>(`/api/v1/retros/${encodeURIComponent(month)}/complete`, {
        method: "POST",
      });
      // Parsed even though the caller reloads via invalidation rather than
      // reading this value -- the useBudget.ts saveMutation precedent: a
      // wire drift on this route's own response shape would otherwise never
      // be caught by anything, since nothing else here parses it.
      return retroWriteResponseSchema.parse(raw).retro;
    },
    onSuccess: afterWrite,
  });

  const discardMutation = useMutation({
    // 204 with no body -- the one status apiFetch does not try to parse
    // (useGoals.ts's deleteContribution precedent). Nothing here reads a
    // response for that reason: the mutationFn's only job is the DELETE
    // call itself.
    mutationFn: async () => {
      await apiFetch<unknown>(`/api/v1/retros/${encodeURIComponent(month)}`, {
        method: "DELETE",
      });
    },
    onSuccess: afterWrite,
  });

  const addActionMutation = useMutation({
    mutationFn: async (body: AddRetroActionBody): Promise<RetroAction> => {
      const raw = await apiFetch<unknown>(`/api/v1/retros/${encodeURIComponent(month)}/actions`, {
        method: "POST",
        body: JSON.stringify({
          body: body.body,
          assigneeMembershipIds: body.assigneeMembershipIds ?? [],
          carriedFrom: body.carriedFrom ?? "",
        }),
      });
      return retroActionResponseSchema.parse(raw).action;
    },
    onSuccess: afterWrite,
  });

  const setActionDoneMutation = useMutation({
    mutationFn: async (vars: { actionId: string; done: boolean }) => {
      const raw = await apiFetch<unknown>(
        `/api/v1/retros/${encodeURIComponent(month)}/actions/${encodeURIComponent(vars.actionId)}`,
        { method: "PATCH", body: JSON.stringify({ done: vars.done }) },
      );
      // Parsed through its own narrower schema (retroActionTickResponseSchema,
      // not retroActionSchema -- retroActionTickResponse's own comment) and
      // discarded: nothing else here reads it, so this is what catches a
      // wire drift on this specific response shape.
      return retroActionTickResponseSchema.parse(raw);
    },
    onSuccess: afterWrite,
  });

  const removeActionMutation = useMutation({
    // 204 with no body, same as discardMutation above.
    mutationFn: async (actionId: string) => {
      await apiFetch<unknown>(
        `/api/v1/retros/${encodeURIComponent(month)}/actions/${encodeURIComponent(actionId)}`,
        { method: "DELETE" },
      );
    },
    onSuccess: afterWrite,
  });

  return {
    data: query.data,
    loading: query.isLoading,
    error: query.error,
    conflict,
    // Clears conflictAt only once the refetch this triggers actually
    // succeeds -- a reload() that itself fails (offline, a 500) has not
    // shown the caller anything current, so there is nothing yet to call
    // resolved.
    reload: async () => {
      const outcome = await query.refetch();
      if (outcome.isSuccess) {
        setConflictAt(null);
      }
    },
    saveRetro: (body: SaveRetroBody) => saveMutation.mutateAsync(body),
    finishRetro: async (): Promise<void> => {
      await finishMutation.mutateAsync();
    },
    discardDraft: async (): Promise<void> => {
      await discardMutation.mutateAsync();
    },
    addAction: (body: AddRetroActionBody): Promise<RetroAction> => addActionMutation.mutateAsync(body),
    setActionDone: async (actionId: string, done: boolean): Promise<void> => {
      await setActionDoneMutation.mutateAsync({ actionId, done });
    },
    removeAction: async (actionId: string): Promise<void> => {
      await removeActionMutation.mutateAsync(actionId);
    },
  };
}
