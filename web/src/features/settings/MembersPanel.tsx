// The Members panel (design/Household Dashboard.dc.html's Settings screen,
// the first card). Renders every member's avatar, name and role/capability
// description exactly as the design writes them, plus an owner-only role
// toggle and per-capability toggles that the static mockup doesn't show --
// nothing in the design demonstrates editing an existing member, but the
// identity spec's whole point for this screen is that an owner can. See the
// per-component comments below for how each control maps onto the backend's
// enforcement, which this UI mirrors rather than duplicates.
//
// Deliberately renders no email column at all, for either an owner or a
// non-owner viewer: the design's own Members list has no email field to
// begin with, so "render accordingly, rather than showing a blank field as
// though the member has no address" (the withheld-email instruction) is
// satisfied by omission -- there is no field here that could be mistaken
// for "this member has no address."
import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { apiErrorMessage } from "../auth/copy";
import { useMe } from "../auth/useAuth";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { ALL_CAPABILITIES } from "./capabilities";
import { InviteMemberModal } from "./InviteMemberModal";
import { memberBadgeLabel, memberDescriptionLine } from "./copy";
import { updateMemberResponseSchema, type MemberView } from "./schemas";
import { householdMembersQueryKey, useHouseholdMembers } from "./useHouseholdMembers";

// api/internal/adapter/http/member_handlers.go's updateMemberRequest fields
// are now pointers (fixed alongside this task, matching /household and
// /notification-preferences): an absent field means "leave this alone,"
// resolved server-side against the membership's *current* role/capabilities
// before domain.ValidateMembershipChange ever runs. This mutation takes
// advantage of that -- role and capabilities are each optional, and only
// the one(s) actually changing are sent. A plain capability toggle
// (toggleCapability below) sends capabilities alone; a role change
// (toggleRole below) sends both, because promoting or demoting genuinely
// changes both fields at once, not because the endpoint demands it.
function useUpdateMember() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: {
      id: string;
      role?: string;
      capabilities?: string[];
    }) => {
      const patch: { role?: string; capabilities?: string[] } = {};
      if (vars.role !== undefined) patch.role = vars.role;
      if (vars.capabilities !== undefined) patch.capabilities = vars.capabilities;

      const body = await apiFetch<unknown>(
        `/api/v1/household/members/${encodeURIComponent(vars.id)}`,
        { method: "PATCH", body: JSON.stringify(patch) },
      );
      return updateMemberResponseSchema.parse(body);
    },
    // Returns the combined invalidation promise rather than firing all
    // three and letting onSuccess return undefined: TanStack Query awaits
    // whatever a mutation's onSuccess returns before treating the mutation
    // as settled, so onSettled (and therefore MembersPanel's pendingIds
    // cleanup, which is what re-enables a member's controls) only runs
    // once every invalidated query has actually refetched. Without the
    // `return`/`Promise.all`, invalidateQueries's returned promises would
    // be fire-and-forget: the PATCH response arriving would settle the
    // mutation immediately, re-enabling the row while ['household',
    // 'members'] was still serving its stale cached value -- the same
    // stale-array race the pendingIds guard exists to close, just moved
    // into the gap between "PATCH resolved" and "refetch landed" instead
    // of "click" and "PATCH resolved".
    onSuccess: () => {
      return Promise.all([
        queryClient.invalidateQueries({ queryKey: householdMembersQueryKey }),
        // The sidebar and every RequireCapability guard read from ['me'];
        // a capability or role change that doesn't refresh it leaves the
        // caller (if they just edited their own membership) looking at
        // stale navigation.
        queryClient.invalidateQueries({ queryKey: ["me"] }),
        // SpacesPanel reads its own, separately keyed ['spaces'] query.
        // domain.VisibleSpaces filters by role/capabilities, so a role or
        // capability change here can change which spaces the caller (if
        // they just edited their own membership -- e.g. an owner demoting
        // themselves) is allowed to see. Without this, SpacesPanel would
        // keep listing a space like Marriage as visible after its own
        // viewer lost the access that used to grant it.
        queryClient.invalidateQueries({ queryKey: ["spaces"] }),
      ]);
    },
  });
}

// Cycled by row index, not read from any field -- the API has no
// per-member colour, see index.css's comment on these tokens. Index 0
// reuses --color-accent, matching the design's own green for its first
// member (Andreas).
const AVATAR_CLASSES = ["bg-accent", "bg-avatar-2", "bg-avatar-3", "bg-avatar-4"];

// The three capabilities a limited member's row may toggle. "marriage" is
// deliberately absent -- domain.ErrLimitedCannotHoldMarriage means the
// server rejects it unconditionally for role "limited", so the control is
// never offered, not merely disabled.
const LIMITED_TOGGLE_CAPS: { key: string; label: string }[] = [
  { key: "calendar", label: "Calendar" },
  { key: "chores", label: "Chores" },
  { key: "money", label: "Money" },
];

function MemberRow({
  member,
  colorClass,
  isOwner,
  pending,
  errorMessage,
  warningMessage,
  onUpdate,
}: {
  member: MemberView;
  colorClass: string;
  isOwner: boolean;
  // True while a mutation for *this* member is in flight. Scoped per
  // member (not a single panel-wide flag) so editing Kayla never disables
  // Ethan's row -- see MembersPanel's pendingIds state for how this is
  // tracked. Needed because toggleCapability below computes its next array
  // from `member.capabilities`, which is only as fresh as the last
  // completed fetch: clicking a second capability before the first PATCH
  // settles and refetches would compute from the pre-first-click array and
  // silently revert it.
  pending: boolean;
  errorMessage?: string;
  warningMessage?: string;
  onUpdate: (vars: { role?: string; capabilities?: string[] }) => void;
}) {
  const isLimited = member.role === "limited";

  // Flips Owner <-> Limited. This is the one control that can reach the
  // server's last-owner rule (domain.ValidateMembershipChange /
  // ErrLastOwner): only a role change away from "owner" ever consults it,
  // never a same-role capability edit. Both fields are sent together here
  // because both are genuinely changing at once -- promoting grants every
  // capability, demoting drops "marriage" -- matching the two invariants
  // domain.NewMembership enforces: an owner must hold every capability, and
  // a limited member may never hold "marriage". (Unlike toggleCapability
  // below, this is not a workaround for the endpoint's own shape -- the
  // server now resolves an omitted field against the current value just
  // fine; this request happens to need both.)
  function toggleRole() {
    if (isLimited) {
      onUpdate({ role: "owner", capabilities: [...ALL_CAPABILITIES] });
    } else {
      onUpdate({
        role: "limited",
        capabilities: member.capabilities.filter((cap) => cap !== "marriage"),
      });
    }
  }

  // Role is never sent here -- it isn't changing, and the server now
  // resolves an absent field against the membership's current role rather
  // than requiring it to be repeated on every request.
  function toggleCapability(cap: string) {
    const next = member.capabilities.includes(cap)
      ? member.capabilities.filter((c) => c !== cap)
      : [...member.capabilities, cap];
    onUpdate({ capabilities: next });
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div
            className={`grid h-[34px] w-[34px] flex-none place-items-center rounded-full text-xs font-semibold text-white ${colorClass}`}
          >
            {member.user.avatarInitial}
          </div>
          <div>
            <div className="text-[13.5px] font-semibold text-ink">
              {member.user.displayName}
            </div>
            <div className="text-[11.5px] text-muted">
              {memberDescriptionLine(member.role, member.capabilities)}
            </div>
          </div>
        </div>

        <button
          type="button"
          role="switch"
          aria-checked={!isLimited}
          aria-label={`${member.user.displayName}'s role`}
          disabled={!isOwner || pending}
          onClick={toggleRole}
          className={`rounded-full px-2.5 py-1 text-[11px] font-semibold disabled:cursor-default ${
            isLimited ? "bg-badge-limited-bg text-label" : "bg-badge-owner-bg text-accent"
          }`}
        >
          {memberBadgeLabel(member.role)}
        </button>
      </div>

      {isOwner && isLimited && (
        <div className="ml-[46px] flex flex-wrap gap-x-5 gap-y-1.5">
          {LIMITED_TOGGLE_CAPS.map(({ key, label }) => (
            <div key={key} className="flex items-center gap-1.5 text-[11px] text-label">
              <ToggleSwitch
                checked={member.capabilities.includes(key)}
                onChange={() => toggleCapability(key)}
                disabled={pending}
                label={`${member.user.displayName} ${label} access`}
              />
              {label}
            </div>
          ))}
        </div>
      )}

      {errorMessage && (
        <p role="alert" className="ml-[46px] text-[11px] text-danger">
          {errorMessage}
        </p>
      )}
      {warningMessage && (
        <p role="status" className="ml-[46px] text-[11px] text-label">
          {warningMessage}
        </p>
      )}
    </div>
  );
}

export function MembersPanel() {
  const me = useMe();
  const members = useHouseholdMembers();
  const updateMember = useUpdateMember();
  const [inviteOpen, setInviteOpen] = useState(false);
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});
  const [rowWarnings, setRowWarnings] = useState<Record<string, string>>({});
  // Which members currently have a mutation in flight -- a Set, not a
  // single boolean, so editing one member's row never disables another's.
  // This is app-level bookkeeping independent of react-query's own
  // mutation.isPending: a single useUpdateMember instance is shared by
  // every row, so its isPending reflects only the most recently dispatched
  // call, not "is a call for member X still outstanding."
  const [pendingIds, setPendingIds] = useState<Set<string>>(new Set());

  const isOwner = me.data?.membership.role === "owner";

  // No optimistic update anywhere in this panel: every toggle's checked
  // state is derived straight from `members.data`, which only changes once
  // the mutation actually succeeds and its query is invalidated/refetched.
  // A 409/422 response therefore leaves every control exactly where it was
  // -- there is no local "already flipped" state to roll back.
  //
  // pendingIds is what stands between a click and the request it sends: a
  // member's role switch and capability toggles are disabled (see
  // MemberRow's `pending` prop) for exactly the window between calling
  // mutate and that mutation settling. Without it, clicking a second
  // capability before the first PATCH resolves and refetches would compute
  // its next array from the same, now-stale `member.capabilities` the first
  // click already read -- silently reverting whichever change happened
  // first.
  function handleUpdate(
    memberId: string,
    vars: { role?: string; capabilities?: string[] },
  ) {
    setRowErrors((prev) => ({ ...prev, [memberId]: "" }));
    setRowWarnings((prev) => ({ ...prev, [memberId]: "" }));
    setPendingIds((prev) => new Set(prev).add(memberId));
    updateMember.mutate(
      { id: memberId, ...vars },
      {
        onSuccess: (result) => {
          if (result.warning) {
            setRowWarnings((prev) => ({ ...prev, [memberId]: result.warning! }));
          }
        },
        onError: (error) => {
          setRowErrors((prev) => ({
            ...prev,
            [memberId]: apiErrorMessage(error, "Something went wrong. Please try again."),
          }));
        },
        onSettled: () => {
          setPendingIds((prev) => {
            const next = new Set(prev);
            next.delete(memberId);
            return next;
          });
        },
      },
    );
  }

  return (
    <section className="rounded-xl border border-hairline bg-card p-[22px]">
      <div className="mb-4 flex items-baseline justify-between">
        <h2 className="text-sm font-semibold text-ink">Members</h2>
        {isOwner && (
          <button
            type="button"
            onClick={() => setInviteOpen(true)}
            // min-h-11/sm:min-h-0: a button this size (no padding at all)
            // falls short of the 44px floor on a phone -- the same gap
            // TransactionFilters.tsx's own SELECT_CLASS comment measures
            // for a padded control; here there's no padding to begin with.
            className="min-h-11 text-xs font-semibold text-accent sm:min-h-0"
          >
            + Invite
          </button>
        )}
      </div>

      {members.isPending && <p className="text-xs text-muted">Loading…</p>}
      {members.isError && (
        <p role="alert" className="text-xs text-danger">
          Couldn't load the members list.
        </p>
      )}

      {members.isSuccess && (
        <div className="flex flex-col gap-3.5">
          {members.data.map((member, index) => (
            <MemberRow
              key={member.id}
              member={member}
              colorClass={AVATAR_CLASSES[index % AVATAR_CLASSES.length]}
              isOwner={isOwner}
              pending={pendingIds.has(member.id)}
              errorMessage={rowErrors[member.id]}
              warningMessage={rowWarnings[member.id]}
              onUpdate={(vars) => handleUpdate(member.id, vars)}
            />
          ))}
        </div>
      )}

      <InviteMemberModal open={inviteOpen} onClose={() => setInviteOpen(false)} />
    </section>
  );
}
