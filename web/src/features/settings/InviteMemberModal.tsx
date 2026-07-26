// The "Invite a family member" modal (design/Household Dashboard.dc.html's
// Settings screen, the modalInvite panel). Owner-only: this screen never
// renders its trigger for anyone else (see MembersPanel.tsx), and
// POST /household/members/invite sits behind requireOwner on the server
// regardless -- this is presentation, not the enforcement.
import { type FormEvent, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Modal } from "../../components/Modal";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { apiFetch } from "../../api/client";
import { apiErrorMessage } from "../auth/copy";
import { ALL_CAPABILITIES } from "./capabilities";

type RoleOption = "owner" | "limited";

async function inviteMember(vars: {
  name: string;
  email: string;
  role: RoleOption;
  capabilities: string[];
}): Promise<{ status: string }> {
  return apiFetch<{ status: string }>("/api/v1/household/members/invite", {
    method: "POST",
    body: JSON.stringify(vars),
  });
}

function useInviteMember() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: inviteMember,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["household", "members"] });
      queryClient.invalidateQueries({ queryKey: ["me"] });
    },
  });
}

export function InviteMemberModal({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  // "Kid" is the design's own default selection for this field.
  const [role, setRole] = useState<RoleOption>("limited");
  // Calendar and chores default on, money default off -- the design's own
  // toggle states ("Off for kids by default" is literal design copy on the
  // money row) and, not coincidentally, exactly what the seed gives Kayla.
  const [limitedCapabilities, setLimitedCapabilities] = useState<string[]>([
    "calendar",
    "chores",
  ]);
  const invite = useInviteMember();

  const capabilities = role === "owner" ? [...ALL_CAPABILITIES] : limitedCapabilities;

  function toggleLimitedCapability(cap: string) {
    setLimitedCapabilities((prev) =>
      prev.includes(cap) ? prev.filter((c) => c !== cap) : [...prev, cap],
    );
  }

  function reset() {
    setName("");
    setEmail("");
    setRole("limited");
    setLimitedCapabilities(["calendar", "chores"]);
    invite.reset();
  }

  function handleClose() {
    reset();
    onClose();
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    invite.mutate(
      { name, email, role, capabilities },
      { onSuccess: handleClose },
    );
  }

  return (
    <Modal open={open} onClose={handleClose} title="Invite a family member">
      <form className="flex flex-col gap-4" onSubmit={handleSubmit}>
        <div className="grid grid-cols-2 gap-4">
          <div className="flex flex-col gap-1.5">
            <label htmlFor="invite-member-name" className="text-xs font-semibold text-label">
              Name
            </label>
            <input
              id="invite-member-name"
              type="text"
              required
              placeholder="First name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <label htmlFor="invite-member-role" className="text-xs font-semibold text-label">
              Role
            </label>
            <select
              id="invite-member-role"
              value={role}
              onChange={(event) => setRole(event.target.value as RoleOption)}
              className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
            >
              <option value="limited">Kid</option>
              <option value="owner">Parent</option>
            </select>
          </div>
        </div>

        <div className="flex flex-col gap-1.5">
          <label htmlFor="invite-member-email" className="text-xs font-semibold text-label">
            {/* The design's own label names a phone option this API has no
                field for -- there is only an email address to send an
                invite to. Kept the literal design copy since the "optional
                for kids" half is accurate (domain.ErrInviteRequiresEmail
                only fires for an owner invite); "or phone" is a known gap,
                not something silently dropped. */}
            Email or phone (optional for kids)
          </label>
          <input
            id="invite-member-email"
            type="email"
            required={role === "owner"}
            placeholder="Send an invite link"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            className="rounded-lg border border-hairline bg-card px-3.5 py-2.5 text-[13.5px]"
          />
        </div>

        <div className="flex flex-col gap-2">
          <span className="text-xs font-semibold text-label">Can access</span>

          <div className="flex items-center justify-between rounded-[10px] border border-hairline px-3.5 py-2.5">
            <span className="text-[13px] text-ink">Calendar</span>
            <ToggleSwitch
              checked={capabilities.includes("calendar")}
              onChange={() => toggleLimitedCapability("calendar")}
              disabled={role === "owner"}
              label="Calendar access"
            />
          </div>

          <div className="flex items-center justify-between rounded-[10px] border border-hairline px-3.5 py-2.5">
            <span className="text-[13px] text-ink">Chores &amp; allowance</span>
            <ToggleSwitch
              checked={capabilities.includes("chores")}
              onChange={() => toggleLimitedCapability("chores")}
              disabled={role === "owner"}
              label="Chores & allowance access"
            />
          </div>

          <div className="flex items-center justify-between rounded-[10px] border border-hairline px-3.5 py-2.5">
            <div>
              <div className="text-[13px] text-ink">Money &amp; balances</div>
              <div className="mt-px text-[11px] text-muted">Off for kids by default</div>
            </div>
            <ToggleSwitch
              checked={capabilities.includes("money")}
              onChange={() => toggleLimitedCapability("money")}
              disabled={role === "owner"}
              label="Money & balances access"
            />
          </div>

          {/* domain.ErrLimitedCannotHoldMarriage: a limited member can never
              hold this capability, so the row is not offered at all (not
              merely disabled-and-off) once "Kid" is selected -- an owner
              row, by contrast, must always hold it, so it renders forced-on
              and disabled rather than omitted. */}
          {role === "owner" && (
            <div className="flex items-center justify-between rounded-[10px] border border-hairline px-3.5 py-2.5">
              <div>
                <div className="text-[13px] text-ink">Marriage space</div>
                <div className="mt-px text-[11px] text-muted">Parents only</div>
              </div>
              <ToggleSwitch checked disabled onChange={() => {}} label="Marriage space access" />
            </div>
          )}
        </div>

        {invite.isError && (
          <p role="alert" className="text-xs leading-snug text-danger">
            {apiErrorMessage(invite.error, "Something went wrong. Please try again.")}
          </p>
        )}

        <div className="mt-1 flex gap-2.5">
          <button
            type="button"
            onClick={handleClose}
            className="flex-1 rounded-lg border border-hairline py-2.5 text-center text-[13px] font-semibold text-label"
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={invite.isPending}
            className="flex-[2] rounded-lg bg-accent py-2.5 text-center text-[13px] font-semibold text-white disabled:cursor-not-allowed disabled:opacity-60"
          >
            Send invite
          </button>
        </div>
      </form>
    </Modal>
  );
}
