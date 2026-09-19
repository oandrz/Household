// The pending half of the Members panel: every invite this household has
// sent that nobody has accepted and that has not expired, each with
// Withdraw (partner-invite spec, milestone 1). Before this an invite was
// written and never read back, so an owner could not tell a sent invite from
// a lost one.
//
// Owner-only, like the route behind it. MembersPanel mounts it only for an
// owner, so a limited member never fires the request at all.
import { useState } from "react";
import { apiErrorMessage } from "../../api/errorMessage";
import { memberBadgeLabel, pendingInviteExpiryLine } from "./copy";
import { type PendingInvite } from "./schemas";
import { usePendingInvites, useWithdrawInvite } from "./usePendingInvites";

function PendingInviteRow({
  invite,
  withdrawing,
  errorMessage,
  onWithdraw,
}: {
  invite: PendingInvite;
  withdrawing: boolean;
  errorMessage?: string;
  onWithdraw: () => void;
}) {
  return (
    <li className="flex flex-col gap-1">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="text-[13.5px] font-semibold text-ink">{invite.name}</div>
          {/* Only the address truncates. At 360px this line is about 214px
              wide, and when the whole line truncated, an ordinary address
              pushed the expiry -- a field the owner needs -- off the end.
              The role and the date keep their full width; the address
              (min-w-0) gives way and ends in an ellipsis. */}
          <div className="flex min-w-0 items-baseline gap-1 text-[11.5px] text-muted">
            <span className="shrink-0">{memberBadgeLabel(invite.role)}</span>
            <span aria-hidden="true" className="shrink-0">
              ·
            </span>
            <span className="min-w-0 truncate">{invite.email}</span>
            <span aria-hidden="true" className="shrink-0">
              ·
            </span>
            <span className="shrink-0">{pendingInviteExpiryLine(invite.expiresAt)}</span>
          </div>
        </div>
        <button
          type="button"
          onClick={onWithdraw}
          disabled={withdrawing}
          aria-label={`Withdraw the invite to ${invite.name}`}
          // min-h-11/sm:min-h-0: MembersPanel's "+ Invite" comment has the
          // reason -- an unpadded text button misses the 44px phone floor.
          className="min-h-11 flex-none text-xs font-semibold text-danger disabled:opacity-50 sm:min-h-0"
        >
          Withdraw
        </button>
      </div>
      {errorMessage && (
        <p role="alert" className="text-[11px] text-danger">
          {errorMessage}
        </p>
      )}
    </li>
  );
}

export function PendingInvitesList() {
  const invites = usePendingInvites({ enabled: true });
  const withdraw = useWithdrawInvite();
  // A Set, not one flag, for the reason MembersPanel's pendingIds gives: one
  // shared mutation's isPending only reflects the latest call. It drives each
  // row's `disabled` Withdraw, and that is what stops a double click sending
  // two DELETEs: React re-renders after a click before the next click event
  // runs, so the second click lands on a disabled button and never fires.
  const [withdrawingIds, setWithdrawingIds] = useState<Set<string>>(new Set());
  const [rowErrors, setRowErrors] = useState<Record<string, string>>({});

  if (invites.isError) {
    return (
      <p role="alert" className="mt-4 text-xs text-danger">
        Couldn't load pending invites.
      </p>
    );
  }
  // Loading and "none pending" both render nothing: a heading over an empty
  // list would be noise on every settled household's Settings page.
  if (!invites.isSuccess || invites.data.length === 0) return null;

  function handleWithdraw(id: string) {
    setRowErrors((prev) => ({ ...prev, [id]: "" }));
    setWithdrawingIds((prev) => new Set(prev).add(id));
    withdraw.mutate(id, {
      onError: (error) => {
        setRowErrors((prev) => ({
          ...prev,
          [id]: apiErrorMessage(error, "Couldn't withdraw that invite. Please try again."),
        }));
      },
      onSettled: () => {
        setWithdrawingIds((prev) => {
          const next = new Set(prev);
          next.delete(id);
          return next;
        });
      },
    });
  }

  return (
    <div className="mt-5 border-t border-hairline pt-4">
      <h3 className="text-xs font-semibold text-label">Pending invites</h3>
      <ul className="mt-2.5 flex flex-col gap-3">
        {invites.data.map((invite) => (
          <PendingInviteRow
            key={invite.id}
            invite={invite}
            withdrawing={withdrawingIds.has(invite.id)}
            errorMessage={rowErrors[invite.id]}
            onWithdraw={() => handleWithdraw(invite.id)}
          />
        ))}
      </ul>
    </div>
  );
}
