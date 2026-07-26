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
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiFetch } from "../../api/client";
import { apiErrorMessage } from "../auth/copy";
import { useMe } from "../auth/useAuth";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { InviteMemberModal } from "./InviteMemberModal";
import { memberBadgeLabel, memberDescriptionLine } from "./copy";
import {
  membersListSchema,
  updateMemberResponseSchema,
  type MemberView,
} from "./schemas";

const membersQueryKey = ["household", "members"] as const;

async function fetchMembers(): Promise<MemberView[]> {
  const body = await apiFetch<unknown>("/api/v1/household/members");
  return membersListSchema.parse(body);
}

function useMembers() {
  return useQuery({ queryKey: membersQueryKey, queryFn: fetchMembers });
}

// api/internal/adapter/http/member_handlers.go's updateMemberRequest has
// plain (non-pointer) Role/Capabilities fields, unlike /household and
// /notification-preferences -- so this is a real PUT-shaped PATCH: every
// call must send both fields, never just the one that changed, or the
// omitted one decodes as its zero value ("" / []) and 422s as an unknown
// role or an invalid capability set.
function useUpdateMember() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { id: string; role: string; capabilities: string[] }) => {
      const body = await apiFetch<unknown>(
        `/api/v1/household/members/${encodeURIComponent(vars.id)}`,
        {
          method: "PATCH",
          body: JSON.stringify({ role: vars.role, capabilities: vars.capabilities }),
        },
      );
      return updateMemberResponseSchema.parse(body);
    },
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: membersQueryKey });
      // The sidebar and every RequireCapability guard read from ['me']; a
      // capability or role change that doesn't refresh it leaves the caller
      // (if they just edited their own membership) looking at stale
      // navigation.
      queryClient.invalidateQueries({ queryKey: ["me"] });
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
  errorMessage,
  warningMessage,
  onUpdate,
}: {
  member: MemberView;
  colorClass: string;
  isOwner: boolean;
  errorMessage?: string;
  warningMessage?: string;
  onUpdate: (vars: { role: string; capabilities: string[] }) => void;
}) {
  const isLimited = member.role === "limited";

  // Flips Owner <-> Limited. This is the one control that can reach the
  // server's last-owner rule (domain.ValidateMembershipChange /
  // ErrLastOwner): only a role change away from "owner" ever consults it,
  // never a same-role capability edit. The capability array is fixed up in
  // the same request rather than left to a second round trip, matching the
  // two invariants domain.NewMembership enforces: an owner must hold every
  // capability, and a limited member may never hold "marriage".
  function toggleRole() {
    if (isLimited) {
      onUpdate({ role: "owner", capabilities: ["calendar", "chores", "money", "marriage"] });
    } else {
      onUpdate({
        role: "limited",
        capabilities: member.capabilities.filter((cap) => cap !== "marriage"),
      });
    }
  }

  function toggleCapability(cap: string) {
    const next = member.capabilities.includes(cap)
      ? member.capabilities.filter((c) => c !== cap)
      : [...member.capabilities, cap];
    onUpdate({ role: member.role, capabilities: next });
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
          disabled={!isOwner}
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
  const members = useMembers();
  const updateMember = useUpdateMember();
  const [inviteOpen, setInviteOpen] = useState(false);
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});
  const [rowWarnings, setRowWarnings] = useState<Record<string, string>>({});

  const isOwner = me.data?.membership.role === "owner";

  // No optimistic update anywhere in this panel: every toggle's checked
  // state is derived straight from `members.data`, which only changes once
  // the mutation actually succeeds and its query is invalidated/refetched.
  // A 409/422 response therefore leaves every control exactly where it was
  // -- there is no local "already flipped" state to roll back.
  function handleUpdate(
    memberId: string,
    vars: { role: string; capabilities: string[] },
  ) {
    setRowErrors((prev) => ({ ...prev, [memberId]: "" }));
    setRowWarnings((prev) => ({ ...prev, [memberId]: "" }));
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
            className="text-xs font-semibold text-accent"
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
