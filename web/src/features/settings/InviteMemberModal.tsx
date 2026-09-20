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
import { useFeature } from "../admin/useFeature";
import { ALL_CAPABILITIES } from "./capabilities";
import { parseEnum } from "../../lib/parseEnum";
import { PendingInviteCard } from "./PendingInviteCard";
import type { PendingInvite } from "./schemas";
import { usePendingInvites } from "./usePendingInvites";
import { ROLE_OPTIONS, type InviteChannel, type RoleOption, useInviteMember } from "./useInviteMember";

// Which channels this role can actually reach, given what `/me` says the
// household's flags allow. "profile" -- a limited member with no sign-in --
// is always available: it is today's kid path, writes no invite row at
// all, and spec decision 9 is explicit that it needs no flag ("Kid
// profiles keep today's path"). The other two depend on useFeature, because
// offering a channel nobody can deliver on is exactly the dead end spec
// decision 11 rules out.
function availableChannelsFor(
  role: RoleOption,
  emailInvites: boolean,
  telegramSignIn: boolean,
): InviteChannel[] {
  if (role === "owner") {
    return [
      ...(telegramSignIn ? (["telegram"] as const) : []),
      ...(emailInvites ? (["email"] as const) : []),
    ];
  }
  return ["profile", ...(telegramSignIn ? (["telegram"] as const) : [])];
}

// "Can sign in (Telegram link)" is the exact wording the design spec gives
// the limited role's own sub-choice; an owner's Telegram option is worded
// plainly instead ("Telegram link"), since an owner is never offered
// "profile" to contrast it with.
function channelLabel(option: InviteChannel, role: RoleOption): string {
  if (option === "profile") return "Profile only (no sign-in)";
  if (option === "email") return "Email";
  return role === "owner" ? "Telegram link" : "Can sign in (Telegram link)";
}

// MembersPanel mounts this only while it is open, so every open is a fresh
// form: there is no reset() to call on close, because the next open starts
// from useState again. `defaultRole` is the role that fresh form starts on.
// It is "limited" (Kid, the design's own default for this field) unless the
// door that opened the modal asks otherwise -- the "Invite your partner"
// links ask for "owner".
export function InviteMemberModal({
  open,
  onClose,
  defaultRole = "limited",
}: {
  open: boolean;
  onClose: () => void;
  defaultRole?: RoleOption;
}) {
  const emailInvites = useFeature("email_invites");
  const telegramSignIn = useFeature("telegram_sign_in");

  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<RoleOption>(defaultRole);
  // A channel the owner picked by hand, from the radio group shown only
  // when this role has more than one available -- undefined until they
  // touch it. Deriving `channel` below from this plus the current role's
  // own available list (rather than storing the resolved channel directly)
  // means a role switch that invalidates a manual pick falls back to that
  // new role's first option on its own, with no separate effect needed to
  // notice and correct it.
  const [manualChannel, setManualChannel] = useState<InviteChannel | undefined>(undefined);
  // Calendar and chores default on, money default off -- the design's own
  // toggle states ("Off for kids by default" is literal design copy on the
  // money row) and, not coincidentally, exactly what the seed gives Kayla.
  const [limitedCapabilities, setLimitedCapabilities] = useState<string[]>([
    "calendar",
    "chores",
  ]);
  // The invite this modal just created, once it has a link to show -- a
  // Telegram invite only (see handleSubmit). Its presence is what turns the
  // form into the waiting card: the link is shown once, so closing on
  // success would throw it away.
  const [created, setCreated] = useState<{ id: string; expiresAt: string; link: string } | null>(
    null,
  );
  const invite = useInviteMember();
  // Only fetched once there is a card to feed. MembersPanel's own
  // PendingInvitesList already polls this exact query key while it is
  // mounted, so this shares that cache rather than doubling the request.
  const pendingInvites = usePendingInvites({ enabled: created !== null });

  const availableChannels = availableChannelsFor(role, emailInvites, telegramSignIn);
  const channel =
    manualChannel && availableChannels.includes(manualChannel) ? manualChannel : availableChannels[0];
  // Only ever true for the owner role: "limited" always has at least
  // "profile" (see availableChannelsFor's own comment).
  const unavailable = availableChannels.length === 0;
  const capabilities = role === "owner" ? [...ALL_CAPABILITIES] : limitedCapabilities;

  function toggleLimitedCapability(cap: string) {
    setLimitedCapabilities((prev) =>
      prev.includes(cap) ? prev.filter((c) => c !== cap) : [...prev, cap],
    );
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    // Unreachable through the UI -- the button this handler answers to
    // isn't rendered while `unavailable` -- kept as a backstop against the
    // browser's own implicit-submit-on-Enter behaviour in a form with a
    // single text field.
    if (!channel) return;
    invite.mutate(
      // The channel is decided here and sent explicitly, never inferred
      // server-side from whether an email field was filled in: the route
      // parses `channel` with a default that refuses (spec, API section),
      // and a UI that left it out would be refused rather than guessed at.
      // That is deliberate -- guessing is how an invite goes to a channel
      // nobody meant.
      { name, email: channel === "email" ? email : "", role, capabilities, channel },
      {
        onSuccess: (result) => {
          if (result.link && result.id && result.expiresAt) {
            setCreated({ id: result.id, expiresAt: result.expiresAt, link: result.link });
          } else {
            // The profile and email paths have no link to lose, so closing
            // is exactly today's behaviour.
            onClose();
          }
        },
      },
    );
  }

  if (created) {
    // The mutation handed back {id, expiresAt, link}, not a PendingInvite,
    // and with no knock -- read the real row out of the list the mutation
    // already invalidated once its refetch lands, and fall back to a row
    // synthesized from what this form just sent until then. Not a second,
    // narrower prop shape for the card: this IS a PendingInvite, just an
    // incomplete one this session already knows enough to fill in.
    const row: PendingInvite =
      pendingInvites.data?.find((candidate) => candidate.id === created.id) ?? {
        id: created.id,
        name,
        role,
        capabilities,
        email: "",
        channel: "telegram",
        knock: null,
        expiresAt: created.expiresAt,
      };
    return (
      <Modal open={open} onClose={onClose} title="Invite a family member">
        <ul className="flex flex-col gap-4">
          <PendingInviteCard
            invite={row}
            link={created.link}
            // Keeps this modal's own copy of the link in sync with whatever
            // the card mints next ("Get a new link"), so a re-render here
            // (or a future read of `created`) never hands the card a stale
            // one. onAdmitted is deliberately not passed: this modal
            // controls its own lifetime and just keeps showing this same
            // card, unlike PendingInvitesList, which needs the notice to
            // outlive the row (see PendingInviteCard.tsx's own comment).
            onNewLink={(link) => setCreated((prev) => (prev ? { ...prev, link } : prev))}
          />
        </ul>
        <div className="mt-4 flex justify-end">
          <button
            type="button"
            onClick={onClose}
            className="min-h-11 rounded-lg border border-hairline px-4 py-2 text-[13px] font-semibold text-label sm:min-h-0"
          >
            Done
          </button>
        </div>
      </Modal>
    );
  }

  return (
    <Modal open={open} onClose={onClose} title="Invite a family member">
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

        {/* Only shown once there is an actual choice to make -- a single
            available channel is auto-selected above with nothing to pick,
            same as the profile-only kid path always was. */}
        {availableChannels.length > 1 && (
          <fieldset className="flex flex-col gap-2">
            <legend className="text-xs font-semibold text-label">
              {role === "owner" ? "Send the invite by" : "How do they sign in?"}
            </legend>
            {availableChannels.map((option) => (
              <label
                key={option}
                className="flex min-h-11 items-center gap-2 text-[13px] text-ink sm:min-h-0"
              >
                <input
                  type="radio"
                  name="invite-member-channel"
                  value={option}
                  checked={channel === option}
                  onChange={() => setManualChannel(option)}
                />
                {channelLabel(option, role)}
              </label>
            ))}
          </fieldset>
        )}

        {channel === "email" && (
          <Field label="Email address" htmlFor="invite-member-email">
            <input
              id="invite-member-email"
              type="email"
              required
              placeholder="Send an invite link"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              className={FIELD_CONTROL_CLASS}
            />
          </Field>
        )}

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

        {unavailable ? (
          // Spec decision 11: say so, rather than offer a button that goes
          // nowhere. The role select above still works -- switching to Kid
          // always has "profile" -- so this only blocks the owner path.
          <div className="flex flex-col gap-3">
            <p className="text-xs leading-snug text-muted">
              Inviting is unavailable on this install. Ask an admin to turn
              on Telegram sign-in or email invites, or invite a kid with a
              profile instead.
            </p>
            {/* "Cancel", not "Close" -- Modal's own X button already carries
                aria-label="Close", and a second control with the same
                accessible name is ambiguous to anyone querying by it. */}
            <button
              type="button"
              onClick={onClose}
              className="min-h-11 self-start rounded-lg border border-hairline px-4 py-2 text-[13px] font-semibold text-label sm:min-h-0"
            >
              Cancel
            </button>
          </div>
        ) : (
          <ModalActions
            secondaryLabel="Cancel"
            onSecondary={onClose}
            primaryLabel="Send invite"
            primaryType="submit"
            primaryDisabled={invite.isPending}
          />
        )}
      </form>
    </Modal>
  );
}
