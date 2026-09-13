// The "Invite a family member" modal (design/Household Dashboard.dc.html's
// Settings screen, the modalInvite panel). Owner-only: this screen never
// renders its trigger for anyone else (see MembersPanel.tsx), and
// POST /household/members/invite sits behind requireOwner on the server
// regardless -- this is presentation, not the enforcement.
import { type FormEvent, useState } from "react";
import { Field } from "../../components/Field";
import { FIELD_CONTROL_CLASS } from "../../components/fieldClasses";
import { FieldPair } from "../../components/FieldPair";
import { Modal } from "../../components/Modal";
import { ModalActions } from "../../components/ModalActions";
import { ToggleSwitch } from "../../components/ToggleSwitch";
import { apiErrorMessage } from "../../api/errorMessage";
import { ALL_CAPABILITIES } from "./capabilities";
import { parseEnum } from "../../lib/parseEnum";
import { ROLE_OPTIONS, type RoleOption, useInviteMember } from "./useInviteMember";

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
        <FieldPair>
          <Field label="Name" htmlFor="invite-member-name">
            <input
              id="invite-member-name"
              type="text"
              required
              placeholder="First name"
              value={name}
              onChange={(event) => setName(event.target.value)}
              className={FIELD_CONTROL_CLASS}
            />
          </Field>
          <Field label="Role" htmlFor="invite-member-role">
            <select
              id="invite-member-role"
              value={role}
              onChange={(event) => setRole(parseEnum(event.target.value, ROLE_OPTIONS, role))}
              className={FIELD_CONTROL_CLASS}
            >
              <option value="limited">Kid</option>
              <option value="owner">Parent</option>
            </select>
          </Field>
        </FieldPair>

        {/* The design's own label names a phone option this API has no
            field for -- there is only an email address to send an
            invite to. Kept the literal design copy since the "optional
            for kids" half is accurate (domain.ErrInviteRequiresEmail
            only fires for an owner invite); "or phone" is a known gap,
            not something silently dropped. */}
        <Field label="Email or phone (optional for kids)" htmlFor="invite-member-email">
          <input
            id="invite-member-email"
            type="email"
            required={role === "owner"}
            placeholder="Send an invite link"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
            className={FIELD_CONTROL_CLASS}
          />
        </Field>

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

        <ModalActions
          secondaryLabel="Cancel"
          onSecondary={handleClose}
          primaryLabel="Send invite"
          primaryType="submit"
          primaryDisabled={invite.isPending}
        />
      </form>
    </Modal>
  );
}
