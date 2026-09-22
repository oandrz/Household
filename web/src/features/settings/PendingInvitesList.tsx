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
import { admittedLine, memberBadgeLabel, pendingInviteExpiryLine } from "./copy";
import { PendingInviteCard } from "./PendingInviteCard";
import { type AdmitResult, type PendingInvite } from "./schemas";
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
  // Keyed by invite id, populated the moment Admit succeeds
  // (PendingInviteCard.tsx's own `onAdmitted` prop) rather than read off
  // `invites.data`, deliberately: Admit's own invalidation removes the row
  // from that list moments later, and PendingInviteCard unmounts along with
  // it. Without this, an owner who admits an invite would see the
  // signInSent: false warning -- the only thing telling them the bot
  // couldn't reach their partner -- disappear along with the row
  // describing it, sometimes before they finish reading it (see
  // PendingInviteCard.tsx's own header comment for the full trace).
  const [admittedResults, setAdmittedResults] = useState<Record<string, AdmitResult>>({});

  if (invites.isError) {
    return (
      <p role="alert" className="mt-4 text-xs text-danger">
        Couldn't load pending invites.
      </p>
    );
  }
  if (!invites.isSuccess) return null;

  const pendingIds = new Set(invites.data.map((invite) => invite.id));
  // Only a result whose row has actually left `invites.data` renders here.
  // In the moment right after Admit succeeds -- before its own refetch
  // lands -- the row (and PendingInviteCard's own state-0 render) is still
  // present, so showing this too would just be a flash of duplicate text;
  // this only takes over once the card describing it is gone.
  const orphanedAdmits = Object.entries(admittedResults).filter(([id]) => !pendingIds.has(id));

  // Loading and "nothing to show" both render nothing: a heading over an
  // empty list would be noise on every settled household's Settings page.
  // "Nothing to show" means zero pending invites AND zero notices still
  // owed -- checking `invites.data.length` alone would hide a just-admitted
  // invite's own warning the moment it was the only row in the list.
  if (invites.data.length === 0 && orphanedAdmits.length === 0) return null;

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
        {invites.data.map((invite) =>
          // A Telegram invite gets the full waiting/knocked/admitted card;
          // an email invite keeps milestone 1's plain row unchanged. This
          // list holds no link -- a link only ever exists in the hand of
          // the session that just minted it (a 201 or a new-link response),
          // and this list observes neither -- so PendingInviteCard gets no
          // `link` prop here.
          invite.channel === "telegram" ? (
            <PendingInviteCard
              key={invite.id}
              invite={invite}
              onAdmitted={(result) =>
                setAdmittedResults((prev) => ({ ...prev, [invite.id]: result }))
              }
            />
          ) : (
            <PendingInviteRow
              key={invite.id}
              invite={invite}
              withdrawing={withdrawingIds.has(invite.id)}
              errorMessage={rowErrors[invite.id]}
              onWithdraw={() => handleWithdraw(invite.id)}
            />
          ),
        )}
        {/* The durable half of the admitted notice -- see admittedResults'
            own comment above. Same card look (name, then the line) as
            PendingInviteCard's state 3, so the handoff from "the card was
            here" to "this notice is here instead" doesn't jump styles. */}
        {orphanedAdmits.map(([id, result]) => (
          <li key={id} className="flex flex-col gap-2 rounded-xl border border-hairline bg-card p-4">
            <div className="text-[13.5px] font-semibold text-ink">{result.member.name}</div>
            <p className="text-[13px] text-ink">{admittedLine(result.signInSent)}</p>
          </li>
        ))}
      </ul>
    </div>
  );
}
